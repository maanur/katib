package metricscollector

import (
	"errors"
	"fmt"
	"strings"
	"time"

	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	v1alpha3 "github.com/kubeflow/katib/pkg/apis/manager/v1alpha3"
	commonv1alpha3 "github.com/kubeflow/katib/pkg/common/v1alpha3"
)

type MetricsCollector struct {
	clientset *kubernetes.Clientset
}

func NewMetricsCollector() (*MetricsCollector, error) {
	config, err := config.GetConfig()
	if err != nil {
		return nil, err
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	return &MetricsCollector{
		clientset: clientset,
	}, nil

}

func (d *MetricsCollector) CollectObservationLog(tId string, jobKind string, metrics []string, namespace string) (*v1alpha3.ObservationLog, error) {
	labelMap := commonv1alpha3.GetJobLabelMap(jobKind, tId)
	pl, err := d.clientset.CoreV1().Pods(namespace).List(metav1.ListOptions{LabelSelector: labels.Set(labelMap).String(), IncludeUninitialized: true})
	if err != nil {
		return nil, err
	}
	if len(pl.Items) == 0 {
		return nil, fmt.Errorf("No Pods are found in Trial %v", tId)
	}
	logopt := apiv1.PodLogOptions{Timestamps: true}
	logs, err := d.clientset.CoreV1().Pods(namespace).GetLogs(pl.Items[0].ObjectMeta.Name, &logopt).Do().Raw()
	if err != nil {
		return nil, err
	}
	if len(logs) == 0 {
		return &v1alpha3.ObservationLog{}, nil
	}
	olog, err := d.parseLogs(tId, strings.Split(string(logs), "\n"), metrics)
	return olog, err
}

func (d *MetricsCollector) parseLogs(tId string, logs []string, metrics []string) (*v1alpha3.ObservationLog, error) {
	var lasterr error
	olog := &v1alpha3.ObservationLog{}
	mlogs := []*v1alpha3.MetricLog{}
	for _, logline := range logs {
		if logline == "" {
			continue
		}
		ls := strings.SplitN(logline, " ", 2)
		if len(ls) != 2 {
			klog.Errorf("Error parsing log: %s", logline)
			lasterr = errors.New("Error parsing log")
			continue
		}
		_, err := time.Parse(time.RFC3339Nano, ls[0])
		if err != nil {
			klog.Errorf("Error parsing time %s: %v", ls[0], err)
			lasterr = err
			continue
		}
		kvpairs := strings.Fields(ls[1])
		for _, kv := range kvpairs {
			v := strings.Split(kv, "=")
			if len(v) > 2 {
				klog.Infof("Ignoring trailing garbage: %s", kv)
			}
			if len(v) == 1 {
				continue
			}
			metricName := ""
			for _, m := range metrics {
				if v[0] == m {
					metricName = v[0]
				}
			}
			if metricName == "" {
				continue
			}
			timestamp := ls[0]
			mlogs = append(mlogs, &v1alpha3.MetricLog{
				TimeStamp: timestamp,
				Metric: &v1alpha3.Metric{
					Name:  metricName,
					Value: v[1],
				},
			})
		}
	}
	olog.MetricLogs = mlogs
	if lasterr != nil {
		return olog, lasterr
	}
	return olog, nil
}
