/*
Copyright 2019 The Tekton Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package resources

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	v1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	"github.com/tektoncd/pipeline/pkg/apis/pipeline/v1beta1"
	"github.com/tektoncd/pipeline/pkg/reconciler/taskrun/resources"
	"github.com/tektoncd/pipeline/pkg/substitution"
	"github.com/tektoncd/pipeline/pkg/workspace"
)

const (
	// resultsParseNumber is the value of how many parts we split from result reference. e.g.  tasks.<taskName>.results.<objectResultName>
	resultsParseNumber = 4
	// objectElementResultsParseNumber is the value of how many parts we split from
	// object attribute result reference. e.g.  tasks.<taskName>.results.<objectResultName>.<individualAttribute>
	objectElementResultsParseNumber = 5
	// objectIndividualVariablePattern is the reference pattern for object individual keys params.<object_param_name>.<key_name>
	objectIndividualVariablePattern = "params.%s.%s"
)

var paramPatterns = []string{
	"params.%s",
	"params[%q]",
	"params['%s']",
}

// ApplyParameters applies the params from a PipelineRun.Params to a PipelineSpec.
func ApplyParameters(ctx context.Context, p *v1.PipelineSpec, pr *v1.PipelineRun) *v1.PipelineSpec {
	// This assumes that the PipelineRun inputs have been validated against what the Pipeline requests.

	// stringReplacements is used for standard single-string stringReplacements,
	// while arrayReplacements/objectReplacements contains arrays/objects that need to be further processed.
	stringReplacements := map[string]string{}
	arrayReplacements := map[string][]string{}
	objectReplacements := map[string]map[string]string{}

	// Set all the default stringReplacements
	for _, p := range p.Params {
		if p.Default != nil {
			switch p.Default.Type {
			case v1.ParamTypeArray:
				for _, pattern := range paramPatterns {
					for i := range len(p.Default.ArrayVal) {
						stringReplacements[fmt.Sprintf(pattern+"[%d]", p.Name, i)] = p.Default.ArrayVal[i]
					}
					arrayReplacements[fmt.Sprintf(pattern, p.Name)] = p.Default.ArrayVal
				}
			case v1.ParamTypeObject:
				for _, pattern := range paramPatterns {
					objectReplacements[fmt.Sprintf(pattern, p.Name)] = p.Default.ObjectVal
				}
				for k, v := range p.Default.ObjectVal {
					stringReplacements[fmt.Sprintf(objectIndividualVariablePattern, p.Name, k)] = v
				}
			case v1.ParamTypeString:
				fallthrough
			default:
				for _, pattern := range paramPatterns {
					stringReplacements[fmt.Sprintf(pattern, p.Name)] = p.Default.StringVal
				}
			}
		}
	}
	// Set and overwrite params with the ones from the PipelineRun
	prStrings, prArrays, prObjects := paramsFromPipelineRun(ctx, pr)

	for k, v := range prStrings {
		stringReplacements[k] = v
	}
	for k, v := range prArrays {
		arrayReplacements[k] = v
	}
	for k, v := range prObjects {
		objectReplacements[k] = v
	}

	return ApplyReplacements(p, stringReplacements, arrayReplacements, objectReplacements)
}

func paramsFromPipelineRun(ctx context.Context, pr *v1.PipelineRun) (map[string]string, map[string][]string, map[string]map[string]string) {
	// stringReplacements is used for standard single-string stringReplacements,
	// while arrayReplacements/objectReplacements contains arrays/objects that need to be further processed.
	stringReplacements := map[string]string{}
	arrayReplacements := map[string][]string{}
	objectReplacements := map[string]map[string]string{}

	for _, p := range pr.Spec.Params {
		switch p.Value.Type {
		case v1.ParamTypeArray:
			for _, pattern := range paramPatterns {
				for i := range len(p.Value.ArrayVal) {
					stringReplacements[fmt.Sprintf(pattern+"[%d]", p.Name, i)] = p.Value.ArrayVal[i]
				}
				arrayReplacements[fmt.Sprintf(pattern, p.Name)] = p.Value.ArrayVal
			}
		case v1.ParamTypeObject:
			for _, pattern := range paramPatterns {
				objectReplacements[fmt.Sprintf(pattern, p.Name)] = p.Value.ObjectVal
			}
			for k, v := range p.Value.ObjectVal {
				stringReplacements[fmt.Sprintf(objectIndividualVariablePattern, p.Name, k)] = v
			}
		case v1.ParamTypeString:
			fallthrough
		default:
			for _, pattern := range paramPatterns {
				stringReplacements[fmt.Sprintf(pattern, p.Name)] = p.Value.StringVal
			}
		}
	}

	return stringReplacements, arrayReplacements, objectReplacements
}

// GetContextReplacements returns the pipelineRun context which can be used to replace context variables in the specifications
func GetContextReplacements(pipelineName string, pr *v1.PipelineRun) map[string]string {
	return map[string]string{
		"context.pipelineRun.name":      pr.Name,
		"context.pipeline.name":         pipelineName,
		"context.pipelineRun.namespace": pr.Namespace,
		"context.pipelineRun.uid":       string(pr.ObjectMeta.UID),
	}
}

// ApplyContexts applies the substitution from $(context.(pipelineRun|pipeline).*) with the specified values.
// Currently supports only name substitution. Uses "" as a default if name is not specified.
func ApplyContexts(spec *v1.PipelineSpec, pipelineName string, pr *v1.PipelineRun) *v1.PipelineSpec {
	for i := range spec.Tasks {
		spec.Tasks[i].DisplayName = substitution.ApplyReplacements(spec.Tasks[i].DisplayName, GetContextReplacements(pipelineName, pr))
	}
	for i := range spec.Finally {
		spec.Finally[i].DisplayName = substitution.ApplyReplacements(spec.Finally[i].DisplayName, GetContextReplacements(pipelineName, pr))
	}
	return ApplyReplacements(spec, GetContextReplacements(pipelineName, pr), map[string][]string{}, map[string]map[string]string{})
}

// filterMatrixContextVar returns a list of params which contain any matrix context variables such as
// $(tasks.<pipelineTaskName>.matrix.length) and $(tasks.<pipelineTaskName>.matrix.<resultName>.length)
func filterMatrixContextVar(params v1.Params) v1.Params {
	var filteredParams v1.Params
	for _, param := range params {
		if expressions, ok := param.GetVarSubstitutionExpressions(); ok {
			for _, expression := range expressions {
				// tasks.<pipelineTaskName>.matrix.length
				// tasks.<pipelineTaskName>.matrix.<resultName>.length
				subExpressions := strings.Split(expression, ".")
				if len(subExpressions) >= 4 && subExpressions[2] == "matrix" && subExpressions[len(subExpressions)-1] == "length" {
					filteredParams = append(filteredParams, param)
				}
			}
		}
	}
	return filteredParams
}

// ApplyPipelineTaskContexts applies the substitution from $(context.pipelineTask.*) with the specified values.
// Uses "0" as a default if a value is not available as well as matrix context variables
// $(tasks.<pipelineTaskName>.matrix.length) and $(tasks.<pipelineTaskName>.matrix.<resultName>.length)
func ApplyPipelineTaskContexts(pt *v1.PipelineTask, pipelineRunStatus v1.PipelineRunStatus, facts *PipelineRunFacts) *v1.PipelineTask {
	pt = pt.DeepCopy()
	var pipelineTaskName string
	var resultName string
	var matrixLength int

	replacements := map[string]string{
		"context.pipelineTask.retries": strconv.Itoa(pt.Retries),
	}

	filteredParams := filterMatrixContextVar(pt.Params)

	for _, p := range filteredParams {
		pipelineTaskName, resultName = p.ParseTaskandResultName()
		// find the referenced pipelineTask to count the matrix combinations
		if pipelineTaskName != "" && pipelineRunStatus.PipelineSpec != nil {
			for _, task := range pipelineRunStatus.PipelineSpec.Tasks {
				if task.Name == pipelineTaskName {
					matrixLength = task.Matrix.CountCombinations()
					replacements["tasks."+pipelineTaskName+".matrix.length"] = strconv.Itoa(matrixLength)
					continue
				}
			}
		}
		// find the resultName from the ResultsCache
		if pipelineTaskName != "" && resultName != "" {
			for _, pt := range facts.State {
				if pt.PipelineTask.Name == pipelineTaskName {
					if len(pt.ResultsCache) == 0 {
						pt.ResultsCache = createResultsCacheMatrixedTaskRuns(pt)
					}
					resultLength := len(pt.ResultsCache[resultName])
					replacements["tasks."+pipelineTaskName+".matrix."+resultName+".length"] = strconv.Itoa(resultLength)
					continue
				}
			}
		}
	}

	pt.Params = pt.Params.ReplaceVariables(replacements, map[string][]string{}, map[string]map[string]string{})
	if pt.IsMatrixed() {
		pt.Matrix.Params = pt.Matrix.Params.ReplaceVariables(replacements, map[string][]string{}, map[string]map[string]string{})
		for i := range pt.Matrix.Include {
			pt.Matrix.Include[i].Params = pt.Matrix.Include[i].Params.ReplaceVariables(replacements, map[string][]string{}, map[string]map[string]string{})
		}
	}
	pt.DisplayName = substitution.ApplyReplacements(pt.DisplayName, replacements)
	return pt
}

// ApplyTaskResults applies the ResolvedResultRef to each PipelineTask.Params and Pipeline.When in targets
func ApplyTaskResults(targets PipelineRunState, resolvedResultRefs ResolvedResultRefs) {
	stringReplacements := resolvedResultRefs.getStringReplacements()
	arrayReplacements := resolvedResultRefs.getArrayReplacements()
	objectReplacements := resolvedResultRefs.getObjectReplacements()
	for _, resolvedPipelineRunTask := range targets {
		if resolvedPipelineRunTask.PipelineTask != nil {
			pipelineTask := resolvedPipelineRunTask.PipelineTask.DeepCopy()
			pipelineTask.Params = pipelineTask.Params.ReplaceVariables(stringReplacements, arrayReplacements, objectReplacements)
			if pipelineTask.IsMatrixed() {
				// Matrixed pipeline results replacements support:
				// 1. String replacements from string, array or object results
				// 2. array replacements from array results are supported
				pipelineTask.Matrix.Params = pipelineTask.Matrix.Params.ReplaceVariables(stringReplacements, arrayReplacements, nil)
				for i := range pipelineTask.Matrix.Include {
					// matrix include parameters can only be type string
					pipelineTask.Matrix.Include[i].Params = pipelineTask.Matrix.Include[i].Params.ReplaceVariables(stringReplacements, nil, nil)
				}
			}
			pipelineTask.When = pipelineTask.When.ReplaceVariables(stringReplacements, arrayReplacements)
			if pipelineTask.TaskRef != nil {
				if pipelineTask.TaskRef.Params != nil {
					pipelineTask.TaskRef.Params = pipelineTask.TaskRef.Params.ReplaceVariables(stringReplacements, arrayReplacements, objectReplacements)
				}
				pipelineTask.TaskRef.Name = substitution.ApplyReplacements(pipelineTask.TaskRef.Name, stringReplacements)
			}
			pipelineTask.DisplayName = substitution.ApplyReplacements(pipelineTask.DisplayName, stringReplacements)
			for i, workspace := range pipelineTask.Workspaces {
				pipelineTask.Workspaces[i].SubPath = substitution.ApplyReplacements(workspace.SubPath, stringReplacements)
			}
			resolvedPipelineRunTask.PipelineTask = pipelineTask
		}
	}
}

// ApplyPipelineTaskStateContext replaces context variables referring to execution status with the specified status
func ApplyPipelineTaskStateContext(state PipelineRunState, replacements map[string]string) {
	for _, resolvedPipelineRunTask := range state {
		if resolvedPipelineRunTask.PipelineTask != nil {
			pipelineTask := resolvedPipelineRunTask.PipelineTask.DeepCopy()
			pipelineTask.Params = pipelineTask.Params.ReplaceVariables(replacements, nil, nil)
			pipelineTask.When = pipelineTask.When.ReplaceVariables(replacements, nil)
			if pipelineTask.TaskRef != nil {
				if pipelineTask.TaskRef.Params != nil {
					pipelineTask.TaskRef.Params = pipelineTask.TaskRef.Params.ReplaceVariables(replacements, nil, nil)
				}
				pipelineTask.TaskRef.Name = substitution.ApplyReplacements(pipelineTask.TaskRef.Name, replacements)
			}
			pipelineTask.DisplayName = substitution.ApplyReplacements(pipelineTask.DisplayName, replacements)
			resolvedPipelineRunTask.PipelineTask = pipelineTask
		}
	}
}

// ApplyWorkspaces replaces workspace variables in the given pipeline spec with their
// concrete values.
func ApplyWorkspaces(p *v1.PipelineSpec, pr *v1.PipelineRun) *v1.PipelineSpec {
	p = p.DeepCopy()
	replacements := map[string]string{}
	for _, declaredWorkspace := range p.Workspaces {
		key := fmt.Sprintf("workspaces.%s.bound", declaredWorkspace.Name)
		replacements[key] = "false"
	}
	for _, boundWorkspace := range pr.Spec.Workspaces {
		key := fmt.Sprintf("workspaces.%s.bound", boundWorkspace.Name)
		replacements[key] = "true"
	}
	return ApplyReplacements(p, replacements, map[string][]string{}, map[string]map[string]string{})
}

// replaceVariablesInPipelineTasks handles variable replacement for a slice of PipelineTasks in-place
func replaceVariablesInPipelineTasks(tasks []v1.PipelineTask, replacements map[string]string,
	arrayReplacements map[string][]string, objectReplacements map[string]map[string]string) {
	for i := range tasks {
		tasks[i].Params = tasks[i].Params.ReplaceVariables(replacements, arrayReplacements, objectReplacements)
		if tasks[i].IsMatrixed() {
			tasks[i].Matrix.Params = tasks[i].Matrix.Params.ReplaceVariables(replacements, arrayReplacements, nil)
			for j := range tasks[i].Matrix.Include {
				tasks[i].Matrix.Include[j].Params = tasks[i].Matrix.Include[j].Params.ReplaceVariables(replacements, nil, nil)
			}
		} else {
			tasks[i].DisplayName = substitution.ApplyReplacements(tasks[i].DisplayName, replacements)
		}
		for j := range tasks[i].Workspaces {
			tasks[i].Workspaces[j].SubPath = substitution.ApplyReplacements(tasks[i].Workspaces[j].SubPath, replacements)
		}
		tasks[i].When = tasks[i].When.ReplaceVariables(replacements, arrayReplacements)
		if tasks[i].TaskRef != nil {
			if tasks[i].TaskRef.Params != nil {
				tasks[i].TaskRef.Params = tasks[i].TaskRef.Params.ReplaceVariables(replacements, arrayReplacements, objectReplacements)
			}
			tasks[i].TaskRef.Name = substitution.ApplyReplacements(tasks[i].TaskRef.Name, replacements)
		}
		tasks[i].OnError = v1.PipelineTaskOnErrorType(substitution.ApplyReplacements(string(tasks[i].OnError), replacements))
		tasks[i] = propagateParams(tasks[i], replacements, arrayReplacements, objectReplacements)
	}
}

// ApplyReplacements replaces placeholders for declared parameters with the specified replacements.
func ApplyReplacements(p *v1.PipelineSpec, replacements map[string]string, arrayReplacements map[string][]string, objectReplacements map[string]map[string]string) *v1.PipelineSpec {
	p = p.DeepCopy()

	// Replace variables in Tasks and Finally tasks
	replaceVariablesInPipelineTasks(p.Tasks, replacements, arrayReplacements, objectReplacements)
	replaceVariablesInPipelineTasks(p.Finally, replacements, arrayReplacements, objectReplacements)

	return p
}

// propagateParams returns a Pipeline Task spec that is the same as the input Pipeline Task spec, but with
// all parameter replacements from `stringReplacements`, `arrayReplacements`, and `objectReplacements` substituted.
// It does not modify `stringReplacements`, `arrayReplacements`, or `objectReplacements`.
func propagateParams(t v1.PipelineTask, stringReplacements map[string]string, arrayReplacements map[string][]string, objectReplacements map[string]map[string]string) v1.PipelineTask {
	if t.TaskSpec == nil {
		return t
	}
	// check if there are task parameters defined that match the params at pipeline level
	if len(t.Params) > 0 {
		stringReplacementsDup := make(map[string]string)
		arrayReplacementsDup := make(map[string][]string)
		objectReplacementsDup := make(map[string]map[string]string)
		for k, v := range stringReplacements {
			stringReplacementsDup[k] = v
		}
		for k, v := range arrayReplacements {
			arrayReplacementsDup[k] = v
		}
		for k, v := range objectReplacements {
			objectReplacementsDup[k] = v
		}
		for _, par := range t.Params {
			for _, pattern := range paramPatterns {
				checkName := fmt.Sprintf(pattern, par.Name)
				// Scoping. Task Params will replace Pipeline Params
				if _, ok := stringReplacementsDup[checkName]; ok {
					stringReplacementsDup[checkName] = par.Value.StringVal
				}
				if _, ok := arrayReplacementsDup[checkName]; ok {
					arrayReplacementsDup[checkName] = par.Value.ArrayVal
				}
				if _, ok := objectReplacementsDup[checkName]; ok {
					objectReplacementsDup[checkName] = par.Value.ObjectVal
					for k, v := range par.Value.ObjectVal {
						stringReplacementsDup[fmt.Sprintf(objectIndividualVariablePattern, par.Name, k)] = v
					}
				}
			}
		}
		t.TaskSpec.TaskSpec = *resources.ApplyReplacements(&t.TaskSpec.TaskSpec, stringReplacementsDup, arrayReplacementsDup, objectReplacementsDup)
	} else {
		t.TaskSpec.TaskSpec = *resources.ApplyReplacements(&t.TaskSpec.TaskSpec, stringReplacements, arrayReplacements, objectReplacements)
	}
	return t
}

// ApplyResultsToWorkspaceBindings applies results from TaskRuns to  WorkspaceBindings in a PipelineRun. It replaces placeholders in
// various binding types with values from TaskRun results.
func ApplyResultsToWorkspaceBindings(trResults map[string][]v1.TaskRunResult, pr *v1.PipelineRun) {
	stringReplacements := map[string]string{}
	for taskName, taskResults := range trResults {
		for _, res := range taskResults {
			switch res.Type {
			case v1.ResultsTypeString:
				stringReplacements[fmt.Sprintf("tasks.%s.results.%s", taskName, res.Name)] = res.Value.StringVal
			case v1.ResultsTypeArray:
				continue
			case v1.ResultsTypeObject:
				for k, v := range res.Value.ObjectVal {
					stringReplacements[fmt.Sprintf("tasks.%s.results.%s.%s", taskName, res.Name, k)] = v
				}
			}
		}
	}

	pr.Spec.Workspaces = workspace.ReplaceWorkspaceBindingsVars(pr.Spec.Workspaces, stringReplacements)
}

// PropagateResults propagate the result of the completed task to the unfinished task that is not explicitly specify in the params
func PropagateResults(rpt *ResolvedPipelineTask, runStates PipelineRunState) {
	if rpt.ResolvedTask == nil || rpt.ResolvedTask.TaskSpec == nil {
		return
	}
	stringReplacements := map[string]string{}
	arrayReplacements := map[string][]string{}
	for taskName, taskResults := range runStates.GetTaskRunsResults() {
		for _, res := range taskResults {
			switch res.Type {
			case v1.ResultsTypeString:
				stringReplacements[fmt.Sprintf("tasks.%s.results.%s", taskName, res.Name)] = res.Value.StringVal
			case v1.ResultsTypeArray:
				arrayReplacements[fmt.Sprintf("tasks.%s.results.%s", taskName, res.Name)] = res.Value.ArrayVal
			case v1.ResultsTypeObject:
				for k, v := range res.Value.ObjectVal {
					stringReplacements[fmt.Sprintf("tasks.%s.results.%s.%s", taskName, res.Name, k)] = v
				}
			}
		}
	}
	rpt.ResolvedTask.TaskSpec = resources.ApplyReplacements(rpt.ResolvedTask.TaskSpec, stringReplacements, arrayReplacements, map[string]map[string]string{})
}

// PropagateArtifacts propagates artifact values from previous task runs into the TaskSpec of the current task.
func PropagateArtifacts(rpt *ResolvedPipelineTask, runStates PipelineRunState) error {
	if rpt.ResolvedTask == nil || rpt.ResolvedTask.TaskSpec == nil {
		return nil
	}
	stringReplacements := map[string]string{}
	for taskName, artifacts := range runStates.GetTaskRunsArtifacts() {
		if artifacts != nil {
			for i, input := range artifacts.Inputs {
				ib, err := json.Marshal(input.Values)
				if err != nil {
					return err
				}
				stringReplacements[fmt.Sprintf("tasks.%s.inputs.%s", taskName, input.Name)] = string(ib)
				if i == 0 {
					stringReplacements[fmt.Sprintf("tasks.%s.inputs", taskName)] = string(ib)
				}
			}
			for i, output := range artifacts.Outputs {
				ob, err := json.Marshal(output.Values)
				if err != nil {
					return err
				}
				stringReplacements[fmt.Sprintf("tasks.%s.outputs.%s", taskName, output.Name)] = string(ob)
				if i == 0 {
					stringReplacements[fmt.Sprintf("tasks.%s.outputs", taskName)] = string(ob)
				}
			}
		}
	}
	rpt.ResolvedTask.TaskSpec = resources.ApplyReplacements(rpt.ResolvedTask.TaskSpec, stringReplacements, map[string][]string{}, map[string]map[string]string{})
	return nil
}

// ApplyTaskResultsToPipelineResults applies the results of completed TasksRuns and Runs to a Pipeline's
// list of PipelineResults, returning the computed set of PipelineRunResults. References to
// non-existent TaskResults or failed TaskRuns or Runs result in a PipelineResult being considered invalid
// and omitted from the returned slice. A nil slice is returned if no results are passed in or all
// results are invalid.
func ApplyTaskResultsToPipelineResults(
	_ context.Context,
	results []v1.PipelineResult,
	taskRunResults map[string][]v1.TaskRunResult,
	customTaskResults map[string][]v1beta1.CustomRunResult,
	taskstatus map[string]string,
) ([]v1.PipelineRunResult, error) {
	var runResults []v1.PipelineRunResult
	var invalidPipelineResults []string

	stringReplacements := map[string]string{}
	arrayReplacements := map[string][]string{}
	objectReplacements := map[string]map[string]string{}
	for _, pipelineResult := range results {
		variablesInPipelineResult, _ := pipelineResult.GetVarSubstitutionExpressions()
		if len(variablesInPipelineResult) == 0 {
			continue
		}
		validPipelineResult := true
		for _, variable := range variablesInPipelineResult {
			if _, isMemoized := stringReplacements[variable]; isMemoized {
				continue
			}
			if _, isMemoized := arrayReplacements[variable]; isMemoized {
				continue
			}
			if _, isMemoized := objectReplacements[variable]; isMemoized {
				continue
			}
			variableParts := strings.Split(variable, ".")

			if (variableParts[0] != v1.ResultTaskPart && variableParts[0] != v1.ResultFinallyPart) || variableParts[2] != v1beta1.ResultResultPart {
				validPipelineResult = false
				invalidPipelineResults = append(invalidPipelineResults, pipelineResult.Name)
				continue
			}
			switch len(variableParts) {
			// For string result: tasks.<taskName>.results.<stringResultName>
			// For array result: tasks.<taskName>.results.<arrayResultName>[*], tasks.<taskName>.results.<arrayResultName>[i]
			// For object result: tasks.<taskName>.results.<objectResultName>[*],
			case resultsParseNumber:
				taskName, resultName := variableParts[1], variableParts[3]
				resultName, stringIdx := v1.ParseResultName(resultName)
				if resultValue := taskResultValue(taskName, resultName, taskRunResults); resultValue != nil {
					switch resultValue.Type {
					case v1.ParamTypeString:
						stringReplacements[variable] = resultValue.StringVal
					case v1.ParamTypeArray:
						if stringIdx != "*" {
							intIdx, _ := strconv.Atoi(stringIdx)
							if intIdx < len(resultValue.ArrayVal) {
								stringReplacements[variable] = resultValue.ArrayVal[intIdx]
							} else {
								// referred array index out of bound
								invalidPipelineResults = append(invalidPipelineResults, pipelineResult.Name)
								validPipelineResult = false
							}
						} else {
							arrayReplacements[substitution.StripStarVarSubExpression(variable)] = resultValue.ArrayVal
						}
					case v1.ParamTypeObject:
						objectReplacements[substitution.StripStarVarSubExpression(variable)] = resultValue.ObjectVal
					}
				} else if resultValue := runResultValue(taskName, resultName, customTaskResults); resultValue != nil {
					stringReplacements[variable] = *resultValue
				} else {
					// if the task is not successful (e.g. skipped or failed) and the results is missing, don't return error
					if status, ok := taskstatus[PipelineTaskStatusPrefix+taskName+PipelineTaskStatusSuffix]; ok {
						if status != v1.TaskRunReasonSuccessful.String() {
							validPipelineResult = false
							continue
						}
					}
					// referred result name is not existent
					invalidPipelineResults = append(invalidPipelineResults, pipelineResult.Name)
					validPipelineResult = false
				}
			// For object type result: tasks.<taskName>.results.<objectResultName>.<individualAttribute>
			case objectElementResultsParseNumber:
				taskName, resultName, objectKey := variableParts[1], variableParts[3], variableParts[4]
				resultName, _ = v1.ParseResultName(resultName)
				if resultValue := taskResultValue(taskName, resultName, taskRunResults); resultValue != nil {
					if _, ok := resultValue.ObjectVal[objectKey]; ok {
						stringReplacements[variable] = resultValue.ObjectVal[objectKey]
					} else {
						// referred object key is not existent
						invalidPipelineResults = append(invalidPipelineResults, pipelineResult.Name)
						validPipelineResult = false
					}
				} else {
					// if the task is not successful (e.g. skipped or failed) and the results is missing, don't return error
					if status, ok := taskstatus[PipelineTaskStatusPrefix+taskName+PipelineTaskStatusSuffix]; ok {
						if status != v1.TaskRunReasonSuccessful.String() {
							validPipelineResult = false
							continue
						}
					}
					// referred result name is not existent
					invalidPipelineResults = append(invalidPipelineResults, pipelineResult.Name)
					validPipelineResult = false
				}
			default:
				invalidPipelineResults = append(invalidPipelineResults, pipelineResult.Name)
				validPipelineResult = false
			}
		}
		if validPipelineResult {
			finalValue := pipelineResult.Value
			finalValue.ApplyReplacements(stringReplacements, arrayReplacements, objectReplacements)
			runResults = append(runResults, v1.PipelineRunResult{
				Name:  pipelineResult.Name,
				Value: finalValue,
			})
		}
	}

	if len(invalidPipelineResults) > 0 {
		return runResults, fmt.Errorf("invalid pipelineresults %v, the referenced results don't exist", invalidPipelineResults)
	}

	return runResults, nil
}

// taskResultValue returns the result value for a given pipeline task name and result name in a map of TaskRunResults for
// pipeline task names. It returns nil if either the pipeline task name isn't present in the map, or if there is no
// result with the result name in the pipeline task name's slice of results.
func taskResultValue(taskName string, resultName string, taskResults map[string][]v1.TaskRunResult) *v1.ResultValue {
	for _, trResult := range taskResults[taskName] {
		if trResult.Name == resultName {
			return &trResult.Value
		}
	}
	return nil
}

// runResultValue returns the result value for a given pipeline task name and result name in a map of RunResults for
// pipeline task names. It returns nil if either the pipeline task name isn't present in the map, or if there is no
// result with the result name in the pipeline task name's slice of results.
func runResultValue(taskName string, resultName string, runResults map[string][]v1beta1.CustomRunResult) *string {
	for _, runResult := range runResults[taskName] {
		if runResult.Name == resultName {
			return &runResult.Value
		}
	}
	return nil
}

// ApplyParametersToWorkspaceBindings applies parameters from PipelineSpec and  PipelineRun to the WorkspaceBindings in a PipelineRun. It replaces
// placeholders in various binding types with values from provided parameters.
func ApplyParametersToWorkspaceBindings(ctx context.Context, pr *v1.PipelineRun) {
	parameters, _, _ := paramsFromPipelineRun(ctx, pr)
	pr.Spec.Workspaces = workspace.ReplaceWorkspaceBindingsVars(pr.Spec.Workspaces, parameters)
}
