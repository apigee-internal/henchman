package henchman

import (
	"fmt"
)

type StandardDeploy struct{}

func (stdDeploy StandardDeploy) ExecuteTasksOnMachines(machines []*Machine, plan *Plan) <-chan error {
	machineChans := []<-chan error{}
	for _, machine := range machines {
		machineChans = append(machineChans, stdDeploy.executeTasks(machine, plan))
	}

	return mergeErrs(machineChans)
}

// Uses plans ManageTaskRun(...)
func (stdDeploy StandardDeploy) executeTasks(machine *Machine, plan *Plan) <-chan error {
	errs := make(chan error, 1)
	registerMap := make(RegMap)
	go func() {
		defer close(errs)
		var actualMachine *Machine
		for _, task := range plan.Tasks {
			if task.Local == true {
				actualMachine = localhost()
			} else {
				actualMachine = machine
			}

			// copy of task.Vars. It'll be different for each machine
			vars := make(VarsMap)
			MergeMap(plan.Vars, vars, true)
			MergeMap(machine.Vars, vars, true)

			if err := task.RenderVars(vars, registerMap); err != nil {
				errs <- HenchErr(err, map[string]interface{}{
					"plan":      plan.Name,
					"task":      task.Name,
					"host":      actualMachine.Hostname,
					"task_vars": task.Vars,
				}, fmt.Sprintf("Error rendering task vars '%s'", task.Name))
				return
			}

			MergeMap(task.Vars, vars, true)
			vars["current_hostname"] = actualMachine.Hostname

			Debug(map[string]interface{}{
				"vars": fmt.Sprintf("%v", vars),
				"plan": plan.Name,
				"task": task.Name,
				"host": actualMachine.Hostname,
			}, "Vars for Task")

			// Checks for subtasks in the with_items field
			subTasks, err := task.ProcessWithItems(vars, registerMap)
			if err != nil {
				errs <- HenchErr(err, map[string]interface{}{
					"plan": plan.Name,
					"task": task.Name,
					"host": actualMachine.Hostname,
				}, fmt.Sprintf("Error generating with_items tasks '%s'", task.Name))
				return
			}

			if subTasks != nil {
				for _, subTask := range subTasks {
					acceptedState, err := plan.ManageTaskRun(subTask, actualMachine, vars, registerMap)
					if !acceptedState {
						if err != nil {
							errs <- err
						}
						return
					}
				}
			} else {
				RenderedTask, err := task.Render(vars, registerMap)
				if err != nil {
					errs <- HenchErr(err, map[string]interface{}{
						"plan": plan.Name,
						"task": RenderedTask.Name,
						"host": actualMachine.Hostname,
					}, fmt.Sprintf("Error rendering task '%s'", RenderedTask.Name))
					return
				}

				// accepted states are ok, success, ignored
				acceptedState, err := plan.ManageTaskRun(RenderedTask, actualMachine, vars, registerMap)
				if !acceptedState {
					if err != nil {
						errs <- err
					}
					return
				}
			}
		}
	}()

	return errs
}
