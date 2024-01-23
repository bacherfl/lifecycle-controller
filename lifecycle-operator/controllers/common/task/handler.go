package task

import (
	"context"
	"fmt"
	keptncontext "github.com/keptn/lifecycle-toolkit/lifecycle-operator/controllers/common/context"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"time"

	"github.com/go-logr/logr"
	klcv1beta1 "github.com/keptn/lifecycle-toolkit/lifecycle-operator/apis/lifecycle/v1beta1"
	apicommon "github.com/keptn/lifecycle-toolkit/lifecycle-operator/apis/lifecycle/v1beta1/common"
	"github.com/keptn/lifecycle-toolkit/lifecycle-operator/controllers/common"
	"github.com/keptn/lifecycle-toolkit/lifecycle-operator/controllers/common/eventsender"
	"github.com/keptn/lifecycle-toolkit/lifecycle-operator/controllers/common/telemetry"
	controllererrors "github.com/keptn/lifecycle-toolkit/lifecycle-operator/controllers/errors"
	"github.com/keptn/lifecycle-toolkit/lifecycle-operator/controllers/lifecycle/interfaces"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

type Handler struct {
	client.Client
	EventSender eventsender.IEvent
	Log         logr.Logger
	Tracer      telemetry.ITracer
	Scheme      *runtime.Scheme
	SpanHandler telemetry.ISpanHandler
}

type CreateTaskAttributes struct {
	SpanName   string
	Definition klcv1beta1.KeptnTaskDefinition
	CheckType  apicommon.CheckType
}

//nolint:gocognit,gocyclo
func (r Handler) ReconcileTasks(ctx context.Context, phaseCtx context.Context, reconcileObject client.Object, taskCreateAttributes CreateTaskAttributes) ([]klcv1beta1.ItemStatus, apicommon.StatusSummary, error) {
	piWrapper, err := interfaces.NewPhaseItemWrapperFromClientObject(reconcileObject)
	if err != nil {
		return nil, apicommon.StatusSummary{}, err
	}

	phase := apicommon.PhaseReconcileTask

	tasks, statuses := r.setupTasks(taskCreateAttributes, piWrapper)

	var summary apicommon.StatusSummary
	summary.Total = len(tasks)
	// Check current state of the PrePostDeploymentTasks
	var newStatus []klcv1beta1.ItemStatus
	for _, taskDefinitionName := range tasks {
		oldstatus := common.GetOldStatus(taskDefinitionName, statuses)

		taskStatus := common.GetItemStatus(taskDefinitionName, statuses)
		task := &klcv1beta1.KeptnTask{}
		taskExists := false

		if oldstatus != taskStatus.Status {
			r.EventSender.Emit(phase, "Normal", reconcileObject, apicommon.PhaseStateStatusChanged, fmt.Sprintf("task status changed from %s to %s", oldstatus, taskStatus.Status), piWrapper.GetVersion())
		}

		// Check if task has already succeeded or failed
		if taskStatus.Status == apicommon.StateSucceeded || taskStatus.Status == apicommon.StateFailed {
			newStatus = append(newStatus, taskStatus)
			continue
		}

		// Check if Task is already created
		if taskStatus.Name != "" {
			err := r.Client.Get(ctx, types.NamespacedName{Name: taskStatus.Name, Namespace: piWrapper.GetNamespace()}, task)
			if err != nil && errors.IsNotFound(err) {
				taskStatus.Name = ""
			} else if err != nil {
				return nil, summary, err
			}
			taskExists = true
		}

		// Create new Task if it does not exist
		if !taskExists {
			err := r.handleTaskNotExists(
				ctx,
				phaseCtx,
				taskCreateAttributes,
				taskDefinitionName,
				piWrapper,
				reconcileObject,
				task,
				&taskStatus,
			)
			if err != nil {
				if errors.IsNotFound(err) {
					r.Log.Info("TaskDefinition for Task not found",
						"task", taskStatus.Name,
						"taskDefinition", taskDefinitionName,
						"namespace", piWrapper.GetNamespace(),
					)
				} else {
					// log the error, but continue to proceed with other tasks that may be created
					r.Log.Error(err, "Could not create task",
						"task", taskStatus.Name,
						"taskDefinition", taskDefinitionName,
						"namespace", piWrapper.GetNamespace(),
					)
				}
				continue
			}
		} else {
			r.handleTaskExists(phaseCtx, task, &taskStatus)
		}
		// Update state of the Check
		newStatus = append(newStatus, taskStatus)
	}

	for _, ns := range newStatus {
		summary = apicommon.UpdateStatusSummary(ns.Status, summary)
	}

	return newStatus, summary, nil
}

//nolint:dupl
func (r Handler) CreateKeptnTask(ctx, phaseCtx context.Context, namespace string, reconcileObject client.Object, taskCreateAttributes CreateTaskAttributes) (string, error) {
	piWrapper, err := interfaces.NewPhaseItemWrapperFromClientObject(reconcileObject)
	if err != nil {
		return "", err
	}

	phase := apicommon.PhaseCreateTask

	newTask := piWrapper.GenerateTask(taskCreateAttributes.Definition, taskCreateAttributes.CheckType)
	injectKeptnContext(phaseCtx, &newTask)
	err = controllerutil.SetControllerReference(reconcileObject, &newTask, r.Scheme)
	if err != nil {
		r.Log.Error(err, "could not set controller reference:")
	}
	err = r.Client.Create(ctx, &newTask)
	if err != nil {
		r.Log.Error(err, "could not create KeptnTask")
		r.EventSender.Emit(phase, "Warning", reconcileObject, apicommon.PhaseStateFailed, "could not create KeptnTask", piWrapper.GetVersion())
		return "", err
	}

	return newTask.Name, nil
}

func injectKeptnContext(phaseCtx context.Context, newTask *klcv1beta1.KeptnTask) {
	if metadata, ok := keptncontext.GetAppMetadataFromContext(phaseCtx); ok {
		traceContextCarrier := &propagation.MapCarrier{}
		otel.GetTextMapPropagator().Inject(phaseCtx, traceContextCarrier)
		newTask.Spec.Context.Metadata = metadata
		for _, key := range traceContextCarrier.Keys() {
			newTask.Spec.Context.Metadata[key] = traceContextCarrier.Get(key)
		}
	}
}

func (r Handler) setTaskFailureEvents(task *klcv1beta1.KeptnTask, spanTrace trace.Span) {
	spanTrace.AddEvent(fmt.Sprintf("task '%s' failed with reason: '%s'", task.Name, task.Status.Message), trace.WithTimestamp(time.Now().UTC()))
}

func (r Handler) setupTasks(taskCreateAttributes CreateTaskAttributes, piWrapper *interfaces.PhaseItemWrapper) ([]string, []klcv1beta1.ItemStatus) {
	var tasks []string
	var statuses []klcv1beta1.ItemStatus

	switch taskCreateAttributes.CheckType {
	case apicommon.PreDeploymentCheckType:
		tasks = piWrapper.GetPreDeploymentTasks()
		statuses = piWrapper.GetPreDeploymentTaskStatus()
	case apicommon.PostDeploymentCheckType:
		tasks = piWrapper.GetPostDeploymentTasks()
		statuses = piWrapper.GetPostDeploymentTaskStatus()
	}
	return tasks, statuses
}

func (r Handler) handleTaskNotExists(ctx context.Context, phaseCtx context.Context, taskCreateAttributes CreateTaskAttributes, taskName string, piWrapper *interfaces.PhaseItemWrapper, reconcileObject client.Object, task *klcv1beta1.KeptnTask, taskStatus *klcv1beta1.ItemStatus) error {
	definition, err := common.GetTaskDefinition(r.Client, r.Log, ctx, taskName, piWrapper.GetNamespace())
	if err != nil {
		return controllererrors.ErrCannotGetKeptnTaskDefinition
	}
	taskCreateAttributes.Definition = *definition
	taskName, err = r.CreateKeptnTask(ctx, phaseCtx, piWrapper.GetNamespace(), reconcileObject, taskCreateAttributes)
	if err != nil {
		return err
	}
	taskStatus.Name = taskName
	taskStatus.SetStartTime()
	_, _, err = r.SpanHandler.GetSpan(phaseCtx, r.Tracer, task, "")
	if err != nil {
		r.Log.Error(err, "could not get span")
	}

	return nil
}

func (r Handler) handleTaskExists(phaseCtx context.Context, task *klcv1beta1.KeptnTask, taskStatus *klcv1beta1.ItemStatus) {
	_, spanTaskTrace, err := r.SpanHandler.GetSpan(phaseCtx, r.Tracer, task, "")
	if err != nil {
		r.Log.Error(err, "could not get span")
	}
	// Update state of Task if it is already created
	taskStatus.Status = task.Status.Status
	if taskStatus.Status.IsCompleted() {
		if taskStatus.Status.IsSucceeded() {
			spanTaskTrace.AddEvent(task.Name + " has finished")
			spanTaskTrace.SetStatus(codes.Ok, "Finished")
		} else {
			spanTaskTrace.AddEvent(task.Name + " has failed")
			r.setTaskFailureEvents(task, spanTaskTrace)
			spanTaskTrace.SetStatus(codes.Error, "Failed")
		}
		spanTaskTrace.End()
		if err := r.SpanHandler.UnbindSpan(task, ""); err != nil {
			r.Log.Error(err, controllererrors.ErrCouldNotUnbindSpan, task.Name)
		}
		taskStatus.SetEndTime()
	}
}
