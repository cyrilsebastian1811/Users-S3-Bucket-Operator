package appservice

import (
	"context"
	"os"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/go-logr/logr"

	appv1alpha1 "github.com/suhas1602/csye7374-operator/pkg/apis/app/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var log = logf.Log.WithName("controller_appservice")

/**
* USER ACTION REQUIRED: This is a scaffold file intended for the user to modify with their own Controller
* business logic.  Delete these comments after modifying this file.*
 */

// Add creates a new AppService Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileAppService{client: mgr.GetClient(), scheme: mgr.GetScheme()}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("appservice-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource AppService
	err = c.Watch(&source.Kind{Type: &appv1alpha1.AppService{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	err = c.Watch(&source.Kind{Type: &corev1.Secret{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &appv1alpha1.AppService{},
	})
	if err != nil {
		return err
	}

	// TODO(user): Modify this to be the types you create that are owned by the primary resource
	// Watch for changes to secondary resource Pods and requeue the owner AppService
	// err = c.Watch(&source.Kind{Type: &corev1.Pod{}}, &handler.EnqueueRequestForOwner{
	// 	IsController: true,
	// 	OwnerType:    &appv1alpha1.AppService{},
	// })
	// if err != nil {
	// 	return err
	// }

	return nil
}

// blank assignment to verify that ReconcileAppService implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileAppService{}

// ReconcileAppService reconciles a AppService object
type ReconcileAppService struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	scheme *runtime.Scheme
}

const appServiceFinalizer = "finalizer.app.example.com"

// Reconcile reads that state of the cluster for a AppService object and makes changes based on the state read
// and what is in the AppService.Spec
// TODO(user): Modify this Reconcile function to implement your Controller logic.  This example creates
// a Pod as an example
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileAppService) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling AppService")

	// Fetch the AppService instance
	instance := &appv1alpha1.AppService{}
	err := r.client.Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	found := &corev1.Secret{}
	err = r.client.Get(context.TODO(), types.NamespacedName{Name: instance.Spec.UserSecret.Name, Namespace: instance.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating new secret")
		secret, err := r.newSecretForCR(instance)
		if err != nil {
			return reconcile.Result{}, err
		}
		err = r.client.Create(context.TODO(), secret)
		if err != nil {
			return reconcile.Result{}, err
		}
		return reconcile.Result{}, nil
	}

	// Check if the AppService instance is marked to be deleted, which is
	// indicated by the deletion timestamp being set.
	isAppServiceMarkedToBeDeleted := instance.GetDeletionTimestamp() != nil
	if isAppServiceMarkedToBeDeleted {
		if contains(instance.GetFinalizers(), appServiceFinalizer) {
			// Run finalization logic for appServiceFinalizer. If the
			// finalization logic fails, don't remove the finalizer so
			// that we can retry during the next reconciliation.
			if err := r.finalizeAppService(reqLogger, instance); err != nil {
				return reconcile.Result{}, err
			}

			// Remove appServiceFinalizer. Once all finalizers have been
			// removed, the object will be deleted.
			instance.SetFinalizers(remove(instance.GetFinalizers(), appServiceFinalizer))
			err := r.client.Update(context.TODO(), instance)
			if err != nil {
				return reconcile.Result{}, err
			}
		}
		return reconcile.Result{}, nil
	}

	// Add finalizer for this CR
	if !contains(instance.GetFinalizers(), appServiceFinalizer) {
		if err := r.addFinalizer(reqLogger, instance); err != nil {
			return reconcile.Result{}, err
		}
	}

	reqLogger.Info("Skip reconcile: Secret already exists", "Secret.Namespace", found.Namespace, "Secret.Name", found.Name)
	return reconcile.Result{}, nil
}

func (r *ReconcileAppService) newSecretForCR(cr *appv1alpha1.AppService) (*corev1.Secret, error) {
	err := createAWSResources(cr)
	if err != nil {
		return nil, err
	}

	labels := map[string]string{
		"app": cr.Name,
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Spec.UserSecret.Name,
			Namespace: cr.Namespace,
			Labels:    labels,
		},
	}

	controllerutil.SetControllerReference(cr, secret, r.scheme)
	return secret, nil
}

func createAWSResources(cr *appv1alpha1.AppService) error {
	sess, err := session.NewSession(&aws.Config{
		Region: aws.String("us-east-1")})

	svc := s3.New(sess)

	bucket, _ := os.LookupEnv("S3_BUCKET")
	folderName := cr.Spec.Username + "/"

	_, err = svc.PutObject(&s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(folderName),
	})
	return err
}

func (r *ReconcileAppService) finalizeAppService(reqLogger logr.Logger, cr *appv1alpha1.AppService) error {
	// TODO(user): Add the cleanup steps that the operator
	// needs to do before the CR can be deleted. Examples
	// of finalizers include performing backups and deleting
	// resources that are not owned by this CR, like a PVC.
	sess, err := session.NewSession(&aws.Config{
		Region: aws.String("us-east-1")})

	svc := s3.New(sess)

	bucket, _ := os.LookupEnv("S3_BUCKET")
	folderName := cr.Spec.Username + "/"

	_, err = svc.DeleteObject(&s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(folderName),
	})
	if err != nil {
		return err
	}

	reqLogger.Info("Successfully finalized AppService")
	return nil
}

func (r *ReconcileAppService) addFinalizer(reqLogger logr.Logger, cr *appv1alpha1.AppService) error {
	reqLogger.Info("Adding Finalizer for the AppService")
	cr.SetFinalizers(append(cr.GetFinalizers(), appServiceFinalizer))

	// Update CR
	err := r.client.Update(context.TODO(), cr)
	if err != nil {
		reqLogger.Error(err, "Failed to update AppService with finalizer")
		return err
	}
	return nil
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func remove(list []string, s string) []string {
	for i, v := range list {
		if v == s {
			list = append(list[:i], list[i+1:]...)
		}
	}
	return list
}
