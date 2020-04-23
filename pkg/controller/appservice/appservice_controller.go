package appservice

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/iam"
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

type policyDocument struct {
	Version   string
	Statement []statementEntry
}

type statementEntry struct {
	Effect    string
	Action    []string
	Resource  string
	Condition json.RawMessage
}

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
		secret, err := r.newSecretForCR(instance, reqLogger)
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

func (r *ReconcileAppService) newSecretForCR(cr *appv1alpha1.AppService, reqLogger logr.Logger) (*corev1.Secret, error) {
	reqLogger.Info("Creating AWS resources")
	accessKey, err := createAWSResources(cr, reqLogger)
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
		StringData: map[string]string{
			"aws_access_key_id":     *accessKey.AccessKeyId,
			"aws_sceret_access_key": *accessKey.SecretAccessKey,
		},
	}

	controllerutil.SetControllerReference(cr, secret, r.scheme)
	return secret, nil
}

func createAWSResources(cr *appv1alpha1.AppService, reqLogger logr.Logger) (*iam.AccessKey, error) {
	sess, err := session.NewSession(&aws.Config{
		Region: aws.String("us-east-1")})

	bucket, _ := os.LookupEnv("S3_BUCKET")
	folderName := cr.Spec.Username + "/"
	reqLogger.Info("Creating S3 Bucket", "Bucket", bucket, "Key", folderName)
	err = createS3Bucket(sess, bucket, folderName)
	if err != nil {
		return nil, err
	}

	reqLogger.Info("Creating IAM resources", "Username", cr.Spec.Username)
	accessKey, err := createIAMReources(sess, cr.Spec.Username, bucket)
	if err != nil {
		return nil, err
	}

	return accessKey, nil
}

func createS3Bucket(sess *session.Session, bucket string, folderName string) error {
	svc := s3.New(sess)

	_, err := svc.PutObject(&s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(folderName),
	})
	return err
}

func createIAMReources(sess *session.Session, username string, bucket string) (*iam.AccessKey, error) {
	svc := iam.New(sess)

	_, err := svc.GetUser(&iam.GetUserInput{UserName: aws.String(username)})

	if awserr, ok := err.(awserr.Error); ok && awserr.Code() == iam.ErrCodeNoSuchEntityException {
		_, err := svc.CreateUser(&iam.CreateUserInput{UserName: aws.String(username)})
		if err != nil {
			return nil, err
		}
	} else {
		return nil, err
	}

	policy := policyDocument{
		Version: "2012-10-17",
		Statement: []statementEntry{
			statementEntry{
				Effect: "Allow",
				Action: []string{
					"s3:ListBucket",
				},
				Resource:  fmt.Sprintf("arn:aws:s3:::%s", bucket),
				Condition: json.RawMessage(fmt.Sprintf(`{"StringLike":{"s3:prefix":["%s/*"]}}`, username)),
			},
			statementEntry{
				Effect: "Allow",
				Action: []string{
					"s3:PutObject",
					"s3:GetObject",
					"s3:DeleteObject",
				},
				Resource:  fmt.Sprintf("arn:aws:s3:::%s/%s/*", bucket, username),
				Condition: json.RawMessage(`{}`),
			},
		},
	}

	b, err := json.Marshal(&policy)
	if err != nil {
		return nil, err
	}

	policyResult, err := svc.CreatePolicy(&iam.CreatePolicyInput{
		PolicyDocument: aws.String(string(b)),
		PolicyName:     aws.String(fmt.Sprintf("%s-S3-Policy", username)),
	})
	if err != nil {
		return nil, err
	}

	_, err = svc.AttachUserPolicy(&iam.AttachUserPolicyInput{
		UserName:  aws.String(username),
		PolicyArn: policyResult.Policy.Arn,
	})
	if err != nil {
		return nil, err
	}

	accessKeyOutput, err := svc.CreateAccessKey(&iam.CreateAccessKeyInput{
		UserName: aws.String(username),
	})
	if err != nil {
		return nil, err
	}

	return accessKeyOutput.AccessKey, nil
}

func (r *ReconcileAppService) finalizeAppService(reqLogger logr.Logger, cr *appv1alpha1.AppService) error {
	// TODO(user): Add the cleanup steps that the operator
	// needs to do before the CR can be deleted. Examples
	// of finalizers include performing backups and deleting
	// resources that are not owned by this CR, like a PVC.

	bucket, _ := os.LookupEnv("S3_BUCKET")
	folderName := cr.Spec.Username + "/"
	reqLogger.Info("Deleting S3 bucket", "Bucket", bucket, "Key", folderName)
	err := deleteS3Bucket(bucket, folderName)
	if err != nil {
		return err
	}

	reqLogger.Info("Deleting IAM resources", "UserName", cr.Spec.Username)
	err = deleteIAMResources(cr.Spec.Username)
	if err != nil {
		return err
	}

	reqLogger.Info("Successfully finalized AppService")
	return nil
}

func deleteS3Bucket(bucket string, key string) error {
	sess, err := session.NewSession(&aws.Config{
		Region: aws.String("us-east-1")})

	svc := s3.New(sess)

	_, err = svc.DeleteObject(&s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})

	return err
}

func deleteIAMResources(username string) error {
	sess, err := session.NewSession(&aws.Config{
		Region: aws.String("us-east-1")})

	svc := iam.New(sess)

	listAccessKeysOutput, _ := svc.ListAccessKeys(&iam.ListAccessKeysInput{
		UserName: aws.String(username),
	})
	if listAccessKeysOutput != nil && len(listAccessKeysOutput.AccessKeyMetadata) > 0 {
		for _, accessKey := range listAccessKeysOutput.AccessKeyMetadata {
			_, err := svc.DeleteAccessKey(&iam.DeleteAccessKeyInput{
				AccessKeyId: accessKey.AccessKeyId,
				UserName:    aws.String(username),
			})
			if err != nil {
				return err
			}
		}
	}

	result, _ := svc.ListAttachedUserPolicies(&iam.ListAttachedUserPoliciesInput{
		UserName: aws.String(username),
	})
	if result != nil && len(result.AttachedPolicies) > 0 {
		for _, policy := range result.AttachedPolicies {
			policyArn := policy.PolicyArn

			_, err := svc.DetachUserPolicy(&iam.DetachUserPolicyInput{
				UserName:  aws.String(username),
				PolicyArn: policyArn,
			})
			if err != nil {
				return err
			}

			_, err = svc.DeletePolicy(&iam.DeletePolicyInput{
				PolicyArn: policyArn,
			})
			if err != nil {
				return err
			}
		}
	}

	_, err = svc.DeleteUser(&iam.DeleteUserInput{
		UserName: aws.String(username),
	})
	return err
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
