// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package controllers

import (
	"context"
	"errors"
	"fmt"
	"time"

	cerrdefs "github.com/containerd/errdefs"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	containersv1alpha1 "github.com/rancher-sandbox/rancher-desktop-daemon/pkg/apis/containers/v1alpha1"
)

const (
	// pullTimeout is the timeout between successive progress updates for a pull.
	pullTimeout = time.Minute
	// terminalRequestTTL is the time to live for a settled ImagePullRequest
	// before it is automatically deleted.
	terminalRequestTTL = 10 * time.Minute
)

// imagePullRequestState tracks the state of an in-progress image pull request.
type imagePullRequestState struct {
	// cancel is the cancel function for the pull context.
	cancel context.CancelCauseFunc
	// desiredReason is the reason to set on the ImagePullRequest's Failed
	// condition on next reconcile.
	desiredReason imagePullRequestFailedReason
	// desiredMessage is the message to set on the ImagePullRequest's Failed
	// condition on next reconcile.
	desiredMessage string
}

type imagePullRequestSettledReason string

const (
	imagePullRequestSettledReasonImagePulled imagePullRequestSettledReason = "ImagePulled"
	imagePullRequestSettledReasonErrored     imagePullRequestSettledReason = "Errored"
	imagePullRequestSettledReasonPulling     imagePullRequestSettledReason = "Pulling"
)

// imagePullRequestFailedReason is the reason for a failed ImagePullRequest.  It
// exists so we can use it as a context cancellation cause, so we can avoid
// overwriting the terminal conditions if the context is canceled for another
// reason.
type imagePullRequestFailedReason string

func (r imagePullRequestFailedReason) Error() string {
	return string(r)
}

const (
	// imagePullRequestFailedReasonZeroValue is an invalid reason; it should never
	// be set on an ImagePullRequest's Failed condition.  It is used to detect the
	// zero value of imagePullRequestReason.
	imagePullRequestFailedReasonZeroValue imagePullRequestFailedReason = ""
	// imagePullRequestFailedReasonPullSucceeded is a non-failure; it indicates
	// that the image pull request completed successfully.  This is never set on a
	// ImagePullRequest's Failed condition.
	imagePullRequestFailedReasonPullSucceeded imagePullRequestFailedReason = "Succeeded"
	// imagePullRequestFailedReasonTimeout indicates that the image pull request
	// timed out due to lack of progress.
	imagePullRequestFailedReasonTimeout imagePullRequestFailedReason = "PullTimeout"
	// imagePullRequestFailedReasonPullFailed indicates that the image pull
	// request failed due to an error from the container engine.
	imagePullRequestFailedReasonPullFailed imagePullRequestFailedReason = "PullFailed"
	// imagePullRequestFailedReasonInvalidArgument indicates that the image pull
	// request failed due to an invalid argument, such as an invalid image reference.
	imagePullRequestFailedReasonInvalidArgument imagePullRequestFailedReason = "InvalidArgument"
	// imagePullRequestFailedReasonUnauthorized indicates that the image pull
	// request failed due to an unauthorized error from the container engine.
	imagePullRequestFailedReasonUnauthorized imagePullRequestFailedReason = "Unauthorized"
	// imagePullRequestFailedReasonDeleted indicates that the image pull request
	// was deleted before it completed.
	imagePullRequestFailedReasonDeleted imagePullRequestFailedReason = "Deleted"
)

// reconcileImagePullRequest handles ImagePullRequest processing for the
// currently selected container engine.
func (r *EngineReconciler) reconcileImagePullRequest(
	ctx context.Context,
	req engineRequest,
) (ctrl.Result, error) {
	r.engineMu.Lock()
	e := r.engine
	r.engineMu.Unlock()

	if req.UID == "" {
		if e == nil {
			// The engine isn't available; we'll rely on the full update when that changes.
			return ctrl.Result{}, nil
		}
		// The engine became available; process all pending image pull requests.
		var imagePullRequestList containersv1alpha1.ImagePullRequestList
		if err := r.List(ctx, &imagePullRequestList, client.InNamespace(req.Namespace)); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to list image pull requests: %w", err)
		}
		var errs []error
		for _, imagePullRequest := range imagePullRequestList.Items {
			if _, err := r.reconcileSingleImagePullRequest(ctx, e, &imagePullRequest); err != nil {
				logf.FromContext(ctx).Error(err, "failed to reconcile image pull request",
					"imagePullRequest", imagePullRequest.Name,
					"repoTag", imagePullRequest.Spec.RepoTag)
				errs = append(errs, err)
			}
		}
		return ctrl.Result{}, errors.Join(errs...)
	}

	var imagePullRequest containersv1alpha1.ImagePullRequest
	if err := r.Get(ctx, req.NamespacedName, &imagePullRequest); err != nil || imagePullRequest.UID != req.UID {
		if client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, err
		}
		// The image pull request was deleted; cancel any in-progress pull.
		r.imagePullRequestMu.Lock()
		state := r.imagePullRequestState[req.UID]
		delete(r.imagePullRequestState, req.UID)
		r.imagePullRequestMu.Unlock()
		if state.cancel != nil {
			state.cancel(imagePullRequestFailedReasonDeleted)
		}
		return ctrl.Result{}, nil
	}

	if e == nil {
		// The engine isn't available; we'll rely on the full update when that changes.
		return ctrl.Result{}, nil
	}
	return r.reconcileSingleImagePullRequest(ctx, e, &imagePullRequest)
}

func (r *EngineReconciler) reconcileSingleImagePullRequest(
	ctx context.Context,
	e engine,
	imagePullRequest *containersv1alpha1.ImagePullRequest,
) (ctrl.Result, error) {
	settledCondition := apimeta.FindStatusCondition(imagePullRequest.Status.Conditions, containersv1alpha1.ImagePullRequestConditionSettled)
	if settledCondition != nil && settledCondition.Status == metav1.ConditionTrue {
		return r.reconcileTerminalImagePullRequest(ctx, imagePullRequest, settledCondition.LastTransitionTime)
	}

	// Handle queued terminal status condition changes.
	r.imagePullRequestMu.Lock()
	state := r.imagePullRequestState[imagePullRequest.UID]
	r.imagePullRequestMu.Unlock()
	if state.desiredReason != imagePullRequestFailedReasonZeroValue {
		if err := r.reconcileImagePullRequestTerminalReason(ctx, imagePullRequest); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to reconcile terminal reason for image pull request: %w", err)
		}
		// Updated to a terminal state; queue for deletion after the TTL.
		return ctrl.Result{RequeueAfter: terminalRequestTTL / 2}, nil
	}

	// This may be triggered from download progress updates; check if a pull is in
	// progress.
	if settledCondition != nil && settledCondition.Reason == string(imagePullRequestSettledReasonPulling) {
		// A pull is in progress; check if it timed out.
		if imagePullRequest.Status.LastUpdateTime.Add(pullTimeout).Before(time.Now().UTC()) {
			cancel := r.setImagePullRequestTerminalReason(
				imagePullRequest,
				imagePullRequestFailedReasonTimeout,
				fmt.Sprintf("image pull request for %q timed out after %v", imagePullRequest.Spec.RepoTag, pullTimeout),
			)
			cancel()
			// Requeue immediately; need to be >0 to trigger a requeue.
			return ctrl.Result{RequeueAfter: 1}, nil
		}
		return ctrl.Result{RequeueAfter: pullTimeout / 2}, nil
	}

	// A pull is _not_ in progress; start one.  Set the status first; if we fail
	// here, the next reconcile will retry.
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var request containersv1alpha1.ImagePullRequest
		err := r.Client.Get(ctx, client.ObjectKeyFromObject(imagePullRequest), &request)
		if err != nil {
			return err
		}
		if apimeta.IsStatusConditionTrue(request.Status.Conditions, containersv1alpha1.ImagePullRequestConditionSettled) {
			// We raced with pull completion; don't overwrite the status.
			return nil
		}
		request.Status.LastUpdateTime = metav1.Now()
		apimeta.SetStatusCondition(&request.Status.Conditions, metav1.Condition{
			Type:               containersv1alpha1.ImagePullRequestConditionSettled,
			Status:             metav1.ConditionFalse,
			ObservedGeneration: request.Generation,
			Reason:             string(imagePullRequestSettledReasonPulling),
			Message:            fmt.Sprintf("pulling image %q", request.Spec.RepoTag),
		})
		return r.Client.Status().Update(ctx, &request)
	})
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update image pull request status after retries: %w", err)
	}

	// Because the image pull request is long-running, use a context that is not
	// cancelled when the reconcile completes.
	pullContext, pullCancel := context.WithCancelCause(context.WithoutCancel(ctx))
	// Cancel the context on shutdown
	context.AfterFunc(r.watcherCtx, func() { pullCancel(r.watcherCtx.Err()) })
	r.imagePullRequestMu.Lock()
	oldState := r.imagePullRequestState[imagePullRequest.UID]
	r.imagePullRequestState[imagePullRequest.UID] = imagePullRequestState{
		cancel: pullCancel,
	}
	r.imagePullRequestMu.Unlock()
	if oldState.cancel != nil {
		// Cancel any previous pull; this could happen if a different reconcile
		// happened in a race, which should be rare.
		oldState.cancel(imagePullRequestFailedReasonPullFailed)
	}

	onProgress := func(start, current, total int64, units string) {
		err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			var request containersv1alpha1.ImagePullRequest
			err := r.Client.Get(pullContext, client.ObjectKeyFromObject(imagePullRequest), &request)
			if err != nil {
				return err
			}
			if apimeta.IsStatusConditionTrue(request.Status.Conditions, containersv1alpha1.ImagePullRequestConditionSettled) {
				// We raced with pull completion; don't overwrite the status.
				return nil
			}
			request.Status.LastUpdateTime = metav1.Now()
			request.Status.Start = start
			request.Status.Current = current
			request.Status.Total = total
			request.Status.Units = units
			apimeta.SetStatusCondition(&request.Status.Conditions, metav1.Condition{
				Type:               containersv1alpha1.ImagePullRequestConditionSettled,
				Status:             metav1.ConditionFalse,
				ObservedGeneration: request.Generation,
				Reason:             "Pulling",
				Message:            fmt.Sprintf("pulling image %q", request.Spec.RepoTag),
			})
			return r.Client.Status().Update(pullContext, &request)
		})
		if err != nil {
			logf.FromContext(pullContext).Error(err, "failed to update image pull request status after retries")
		}
	}
	onComplete := func(err error) {
		r.imagePullRequestMu.Lock()
		state := r.imagePullRequestState[imagePullRequest.UID]
		r.imagePullRequestMu.Unlock()
		if state.desiredReason != imagePullRequestFailedReasonZeroValue {
			// The desired reason was already set; don't overwrite it.
			return
		}
		if _, ok := errors.AsType[imagePullRequestFailedReason](context.Cause(pullContext)); ok {
			// The pull was cancelled, and we already handled any pending reasons.
			return
		}
		var latest containersv1alpha1.ImagePullRequest
		getErr := r.Get(pullContext, client.ObjectKeyFromObject(imagePullRequest), &latest)
		if getErr == nil {
			if apimeta.IsStatusConditionTrue(latest.Status.Conditions, containersv1alpha1.ImagePullRequestConditionSettled) {
				// The request was already settled; don't overwrite the status.
				return
			}
		} else if apierrors.IsNotFound(getErr) {
			// The request has been deleted; skip doing anything.
			return
		} else {
			logf.FromContext(pullContext).V(2).Info(
				"failed to check for settled image pull request; continuing anyway",
				"error", getErr,
			)
		}
		reason := imagePullRequestFailedReasonPullFailed
		message := fmt.Sprintf("failed to pull image %q: %v", imagePullRequest.Spec.RepoTag, err)
		switch {
		case cerrdefs.IsInvalidArgument(err):
			reason = imagePullRequestFailedReasonInvalidArgument
		case cerrdefs.IsUnauthorized(err):
			reason = imagePullRequestFailedReasonUnauthorized
		case err == nil:
			reason = imagePullRequestFailedReasonPullSucceeded
			message = "image pulled successfully"
		}
		cancel := r.setImagePullRequestTerminalReason(imagePullRequest, reason, message)
		defer cancel()
		// Force update the change synchronously, as otherwise we will need to wait
		// for the next requeued reconcile (up to pullTimeout/2).
		err = r.reconcileImagePullRequestTerminalReason(pullContext, imagePullRequest)
		if err != nil {
			logf.FromContext(pullContext).Error(err,
				"failed to reconcile image pull request terminal reason, waiting for requeue",
				"namespace", imagePullRequest.GetNamespace(),
				"name", imagePullRequest.GetName())
		}
	}

	// Actually start the pull asynchronously.
	err = e.pullImage(pullContext, imagePullRequest.Spec.RepoTag, onProgress, onComplete)
	if err != nil {
		cancel := r.setImagePullRequestTerminalReason(
			imagePullRequest,
			imagePullRequestFailedReasonPullFailed,
			fmt.Sprintf("failed to pull image %q: %v", imagePullRequest.Spec.RepoTag, err),
		)
		cancel()
		return ctrl.Result{RequeueAfter: time.Second}, nil
	}

	logf.FromContext(ctx).V(1).Info("Image pull started",
		"imagePullRequest", imagePullRequest.Name,
		"repoTag", imagePullRequest.Spec.RepoTag)
	return ctrl.Result{RequeueAfter: pullTimeout / 2}, nil
}

// reconcileImagePullRequestTerminalReason applies the desired terminal reason
// for a failed ImagePullRequest that was previously set by
// setImagePullRequestTerminalReason.
func (r *EngineReconciler) reconcileImagePullRequestTerminalReason(
	ctx context.Context,
	imagePullRequest *containersv1alpha1.ImagePullRequest,
) error {
	key := client.ObjectKeyFromObject(imagePullRequest)
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latest := &containersv1alpha1.ImagePullRequest{}
		if err := r.Get(ctx, key, latest); err != nil {
			return client.IgnoreNotFound(err)
		}
		if latest.UID != imagePullRequest.UID {
			// The request was deleted and recreated; don't apply the reason.
			return nil
		}

		r.imagePullRequestMu.Lock()
		state := r.imagePullRequestState[imagePullRequest.UID]
		r.imagePullRequestMu.Unlock()
		if state.desiredReason == imagePullRequestFailedReasonZeroValue {
			// The desired reason was already applied; nothing to do.
			return nil
		}

		latest.Status.LastUpdateTime = metav1.Now()

		settledReason := imagePullRequestSettledReasonImagePulled
		failedStatus := metav1.ConditionFalse
		failedReason := imagePullRequestFailedReasonPullSucceeded
		if state.desiredReason != imagePullRequestFailedReasonPullSucceeded {
			settledReason = imagePullRequestSettledReasonErrored
			failedStatus = metav1.ConditionTrue
			failedReason = state.desiredReason
		} else {
			// Pull succeeded; assume current progress is total.
			latest.Status.Current = latest.Status.Total
		}

		apimeta.SetStatusCondition(&latest.Status.Conditions, metav1.Condition{
			Type:               containersv1alpha1.ImagePullRequestConditionSettled,
			Status:             metav1.ConditionTrue,
			ObservedGeneration: latest.Generation,
			Reason:             string(settledReason),
			Message:            state.desiredMessage,
		})
		apimeta.SetStatusCondition(&latest.Status.Conditions, metav1.Condition{
			Type:               containersv1alpha1.ImagePullRequestConditionFailed,
			Status:             failedStatus,
			ObservedGeneration: latest.Generation,
			Reason:             string(failedReason),
			Message:            state.desiredMessage,
		})

		return r.Status().Update(ctx, latest)
	})
	if err == nil {
		// The reason was applied successfully.
		r.imagePullRequestMu.Lock()
		delete(r.imagePullRequestState, imagePullRequest.UID)
		r.imagePullRequestMu.Unlock()
	}
	return err
}

// setImagePullRequestTerminalReason sets the desired reason for a failed
// ImagePullRequest.  The next reconcile will set the terminal conditions on the
// request.  It returns a function to cancel the pull context, if one is in
// progress, or a no-op function if there is no pull in progress.
func (r *EngineReconciler) setImagePullRequestTerminalReason(
	imagePullRequest *containersv1alpha1.ImagePullRequest,
	reason imagePullRequestFailedReason,
	message string,
) context.CancelFunc {
	var cancel context.CancelCauseFunc
	r.imagePullRequestMu.Lock()
	state := r.imagePullRequestState[imagePullRequest.UID]
	cancel = state.cancel
	state.desiredReason = reason
	state.desiredMessage = message
	state.cancel = nil
	r.imagePullRequestState[imagePullRequest.UID] = state
	r.imagePullRequestMu.Unlock()
	if cancel != nil {
		return func() { cancel(reason) }
	}
	return func() {}
}

// reconcileTerminalImagePullRequest handles ImagePullRequest processing for
// requests that have already been settled. It ensures the TTL has elapsed, then
// deletes the request if it has no owner references.
func (r *EngineReconciler) reconcileTerminalImagePullRequest(
	ctx context.Context,
	imagePullRequest *containersv1alpha1.ImagePullRequest,
	lastTransitionTime metav1.Time,
) (ctrl.Result, error) {
	if lastTransitionTime.Add(terminalRequestTTL).After(time.Now().UTC()) {
		// The request has not yet expired; requeue for the remaining time.
		return ctrl.Result{RequeueAfter: time.Until(lastTransitionTime.Add(terminalRequestTTL))}, nil
	}

	// Do not delete the request if it has an owner reference; the owner probably
	// wants to read the result.
	if len(imagePullRequest.OwnerReferences) > 0 {
		return ctrl.Result{}, nil
	}

	if err := r.Delete(ctx, imagePullRequest); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	r.imagePullRequestMu.Lock()
	delete(r.imagePullRequestState, imagePullRequest.UID)
	r.imagePullRequestMu.Unlock()
	return ctrl.Result{}, nil
}
