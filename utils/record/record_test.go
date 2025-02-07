package record

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/argoproj/notifications-engine/pkg/api"
	"github.com/argoproj/notifications-engine/pkg/mocks"
	"github.com/argoproj/notifications-engine/pkg/services"
	"github.com/argoproj/notifications-engine/pkg/triggers"
	"github.com/golang/mock/gomock"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

func TestRecordLog(t *testing.T) {
	prevOutput := log.StandardLogger().Out
	defer func() {
		log.SetOutput(prevOutput)
	}()

	buf := bytes.NewBufferString("")
	log.SetOutput(buf)

	r := v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "guestbook",
			Namespace: "default",
		},
	}
	rec := NewFakeEventRecorder()
	rec.Eventf(&r, EventOptions{EventReason: "FooReason"}, "Rollout is %s", "foo")

	logMessage := buf.String()
	assert.True(t, strings.Contains(logMessage, "level=info"))
	assert.True(t, strings.Contains(logMessage, "namespace=default"))
	assert.True(t, strings.Contains(logMessage, "rollout=guestbook"))
	assert.True(t, strings.Contains(logMessage, "event_reason=FooReason"))
	assert.True(t, strings.Contains(logMessage, "Rollout is foo"))

	buf = bytes.NewBufferString("")
	log.SetOutput(buf)
	rec.Warnf(&r, EventOptions{EventReason: "FooReason"}, "Rollout is %s", "foo")
	logMessage = buf.String()
	fmt.Println(logMessage)
	assert.True(t, strings.Contains(logMessage, "level=warning"))

	buf = bytes.NewBufferString("")
	log.SetOutput(buf)
	rec.Eventf(&r, EventOptions{EventType: "Warning", EventReason: "FooReason"}, "Rollout is %s", "foo")
	logMessage = buf.String()
	fmt.Println(logMessage)
	assert.True(t, strings.Contains(logMessage, "level=warning"))

}

func TestIncCounter(t *testing.T) {
	r := v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "guestbook",
			Namespace: "default",
		},
	}
	rec := NewFakeEventRecorder()
	for i := 0; i < 3; i++ {
		rec.Eventf(&r, EventOptions{EventReason: "FooReason"}, "something happened")
	}
	ch := make(chan prometheus.Metric, 1)
	rec.RolloutEventCounter.Collect(ch)
	m := <-ch
	buf := dto.Metric{}
	m.Write(&buf)
	assert.Equal(t, float64(3), *buf.Counter.Value)
	assert.Equal(t, []string{"FooReason", "FooReason", "FooReason"}, rec.Events)
}

func TestSendNotifications(t *testing.T) {
	r := v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "guestbook",
			Namespace:   "default",
			Annotations: map[string]string{"notifications.argoproj.io/subscribe.on-foo-reason.console": "console"},
		},
	}
	mockCtrl := gomock.NewController(t)
	mockAPI := mocks.NewMockAPI(mockCtrl)
	mockAPI.EXPECT().Send(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	mockAPI.EXPECT().GetConfig().Return(api.Config{
		Triggers: map[string][]triggers.Condition{"on-foo-reason": {triggers.Condition{Send: []string{"my-template"}}}}}).AnyTimes()
	apiFactory := &mocks.FakeFactory{Api: mockAPI}
	rec := NewFakeEventRecorder()
	rec.EventRecorderAdapter.apiFactory = apiFactory
	//ch := make(chan prometheus.HistogramVec, 1)
	err := rec.sendNotifications(&r, EventOptions{EventReason: "FooReason"})
	assert.NoError(t, err)
}

func TestNotificationFailedCounter(t *testing.T) {
	r := v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "guestbook",
			Namespace:   "default",
			Annotations: map[string]string{"notifications.argoproj.io/subscribe.on-foo-reason.console": "console"},
		},
	}
	rec := NewFakeEventRecorder()
	opts := EventOptions{EventType: corev1.EventTypeWarning, EventReason: "FooReason"}
	rec.NotificationFailedCounter.WithLabelValues(r.Name, r.Namespace, opts.EventType, opts.EventReason).Inc()

	res := testutil.ToFloat64(rec.NotificationFailedCounter)
	assert.Equal(t, float64(1), res)
}

func TestNotificationSuccessCounter(t *testing.T) {
	r := v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "guestbook",
			Namespace:   "default",
			Annotations: map[string]string{"notifications.argoproj.io/subscribe.on-foo-reason.console": "console"},
		},
	}
	rec := NewFakeEventRecorder()
	opts := EventOptions{EventType: corev1.EventTypeNormal, EventReason: "FooReason"}
	rec.NotificationSuccessCounter.WithLabelValues(r.Name, r.Namespace, opts.EventType, opts.EventReason).Inc()

	res := testutil.ToFloat64(rec.NotificationSuccessCounter)
	assert.Equal(t, float64(1), res)
}

func TestNotificationSendPerformance(t *testing.T) {
	r := v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "guestbook",
			Namespace:   "default",
			Annotations: map[string]string{"notifications.argoproj.io/subscribe.on-foo-reason.console": "console"},
		},
	}
	rec := NewFakeEventRecorder()
	rec.NotificationSendPerformance.WithLabelValues(r.Namespace, r.Name).Observe(float64(0.4))
	rec.NotificationSendPerformance.WithLabelValues(r.Namespace, r.Name).Observe(float64(1.3))
	rec.NotificationSendPerformance.WithLabelValues(r.Namespace, r.Name).Observe(float64(0.5))
	rec.NotificationSendPerformance.WithLabelValues(r.Namespace, r.Name).Observe(float64(1.4))
	rec.NotificationSendPerformance.WithLabelValues(r.Namespace, r.Name).Observe(float64(0.6))
	rec.NotificationSendPerformance.WithLabelValues(r.Namespace, r.Name).Observe(float64(0.1))
	rec.NotificationSendPerformance.WithLabelValues(r.Namespace, r.Name).Observe(float64(1.3))
	rec.NotificationSendPerformance.WithLabelValues(r.Namespace, r.Name).Observe(float64(0.25))
	rec.NotificationSendPerformance.WithLabelValues(r.Namespace, r.Name).Observe(float64(0.9))
	rec.NotificationSendPerformance.WithLabelValues(r.Namespace, r.Name).Observe(float64(0.17))
	rec.NotificationSendPerformance.WithLabelValues(r.Namespace, r.Name).Observe(float64(0.35))

	reg := prometheus.NewRegistry()
	reg.MustRegister(rec.NotificationSendPerformance)

	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	log.Infof("mfs: %v, %v, %v, %v", *mfs[0], *mfs[0].Metric[0].Histogram.SampleCount, *mfs[0].Metric[0].Histogram.SampleSum, *mfs[0].Metric[0].Histogram.Bucket[0].CumulativeCount)
	want := `# HELP notification_send_performance Notification send performance.
			 # TYPE notification_send_performance histogram
			 notification_send_performance_bucket{name="guestbook",namespace="default",le="0.01"} 0
 			 notification_send_performance_bucket{name="guestbook",namespace="default",le="0.15"} 1
			 notification_send_performance_bucket{name="guestbook",namespace="default",le="0.25"} 3
			 notification_send_performance_bucket{name="guestbook",namespace="default",le="0.5"} 6
			 notification_send_performance_bucket{name="guestbook",namespace="default",le="1"} 8
			 notification_send_performance_bucket{name="guestbook",namespace="default",le="+Inf"} 11
			 notification_send_performance_sum{name="guestbook",namespace="default"} 7.27
			 notification_send_performance_count{name="guestbook",namespace="default"} 11
			 `
	err = testutil.CollectAndCompare(rec.NotificationSendPerformance, strings.NewReader(want))
	assert.Nil(t, err)
}

func TestSendNotificationsFails(t *testing.T) {
	r := v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "guestbook",
			Namespace:   "default",
			Annotations: map[string]string{"notifications.argoproj.io/subscribe.on-foo-reason.console": "console"},
		},
	}

	t.Run("SendError", func(t *testing.T) {
		mockCtrl := gomock.NewController(t)
		mockAPI := mocks.NewMockAPI(mockCtrl)
		mockAPI.EXPECT().Send(gomock.Any(), gomock.Any(), gomock.Any()).Return(fmt.Errorf("failed to send")).AnyTimes()
		mockAPI.EXPECT().GetConfig().Return(api.Config{
			Triggers: map[string][]triggers.Condition{"on-foo-reason": {triggers.Condition{Send: []string{"my-template"}}}}}).AnyTimes()
		apiFactory := &mocks.FakeFactory{Api: mockAPI}
		rec := NewFakeEventRecorder()
		rec.EventRecorderAdapter.apiFactory = apiFactory

		err := rec.sendNotifications(&r, EventOptions{EventReason: "FooReason"})
		assert.Error(t, err)
	})

	t.Run("GetAPIError", func(t *testing.T) {
		apiFactory := &mocks.FakeFactory{Err: errors.New("failed to get API")}
		rec := NewFakeEventRecorder()
		rec.EventRecorderAdapter.apiFactory = apiFactory

		err := rec.sendNotifications(&r, EventOptions{EventReason: "FooReason"})
		assert.Error(t, err)
	})

}

func TestSendNotificationsNoTrigger(t *testing.T) {
	r := v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "guestbook",
			Namespace:   "default",
			Annotations: map[string]string{"notifications.argoproj.io/subscribe.on-missing-reason.console": "console"},
		},
	}

	mockCtrl := gomock.NewController(t)
	mockAPI := mocks.NewMockAPI(mockCtrl)
	mockAPI.EXPECT().GetConfig().Return(api.Config{
		Triggers: map[string][]triggers.Condition{"on-foo-reason": {triggers.Condition{Send: []string{"my-template"}}}}}).AnyTimes()
	mockAPI.EXPECT().Send(gomock.Any(), gomock.Any(), gomock.Any()).Return(fmt.Errorf("failed to send")).Times(0)
	apiFactory := &mocks.FakeFactory{Api: mockAPI}
	rec := NewFakeEventRecorder()
	rec.EventRecorderAdapter.apiFactory = apiFactory

	err := rec.sendNotifications(&r, EventOptions{EventReason: "MissingReason"})
	assert.NoError(t, err)
}

func TestNewAPIFactorySettings(t *testing.T) {
	settings := NewAPIFactorySettings()
	assert.Equal(t, NotificationConfigMap, settings.ConfigMapName)
	assert.Equal(t, NotificationSecret, settings.SecretName)
	getVars, err := settings.InitGetVars(nil, nil, nil)
	assert.NoError(t, err)

	rollout := map[string]interface{}{"name": "hello"}
	vars := getVars(rollout, services.Destination{})

	assert.Equal(t, map[string]interface{}{"rollout": rollout}, vars)
}

func TestWorkloadRefObjectMap(t *testing.T) {
	ro := v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "guestbook",
			Namespace:   "default",
			Annotations: map[string]string{"notifications.argoproj.io/subscribe.on-missing-reason.console": "console"},
		},
		Spec: v1alpha1.RolloutSpec{
			TemplateResolvedFromRef: true,
			SelectorResolvedFromRef: true,
			WorkloadRef: &v1alpha1.ObjectRef{
				Kind:       "Deployment",
				Name:       "foo",
				APIVersion: "apps/v1",
			},
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "foo",
						},
					},
				},
			},
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"foo": "bar",
				},
			},
		},
	}
	objMap, err := toObjectMap(&ro)
	assert.NoError(t, err)

	templateMap, ok, err := unstructured.NestedMap(objMap, "spec", "template")
	assert.NoError(t, err)
	assert.True(t, ok)
	assert.NotNil(t, templateMap)

	selectorMap, ok, err := unstructured.NestedMap(objMap, "spec", "selector")
	assert.NoError(t, err)
	assert.True(t, ok)
	assert.NotNil(t, selectorMap)
}
