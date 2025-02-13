// Copyright The OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/spf13/pflag"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	"sigs.k8s.io/controller-runtime/pkg/client"

	otelv1alpha1 "github.com/open-telemetry/opentelemetry-operator/apis/v1alpha1"
)

var scheme *k8sruntime.Scheme

func init() {
	scheme = k8sruntime.NewScheme()
	utilruntime.Must(otelv1alpha1.AddToScheme(scheme))
	utilruntime.Must(appsv1.AddToScheme(scheme))
}

func main() {
	var timeout int
	var kubeconfigPath string

	defaultKubeconfigPath := filepath.Join(homedir.HomeDir(), ".kube", "config")

	pflag.IntVar(&timeout, "timeout", 300, "The timeout for the check.")
	pflag.StringVar(&kubeconfigPath, "kubeconfig-path", defaultKubeconfigPath, "Absolute path to the KubeconfigPath file")
	pflag.Parse()

	pollInterval := 500 * time.Millisecond
	timeoutPoll := time.Duration(timeout) * time.Second

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		println("Error reading the kubeconfig:", err.Error())
		os.Exit(1)
	}

	clusterClient, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		println("Creating the Kubernetes client", err)
		os.Exit(1)
	}

	fmt.Println("Waiting until the OTEL Collector Operator is deployed")
	operatorDeployment := &appsv1.Deployment{}

	err = wait.Poll(pollInterval, timeoutPoll, func() (done bool, err error) {
		err = clusterClient.Get(
			context.Background(),
			client.ObjectKey{
				Name:      "opentelemetry-operator-controller-manager",
				Namespace: "opentelemetry-operator-system",
			},
			operatorDeployment,
		)
		if err != nil {
			fmt.Println(err)
			return false, nil
		}
		return true, nil
	})

	if err != nil {
		fmt.Println(err)
	}
	fmt.Println("OTEL Collector Operator is deployed properly!")

	// Sometimes, the deployment of the OTEL Operator is ready but, when
	// creating new instances of the OTEL Collector, the webhook is not reachable
	// and kubectl apply fails. This code deployes an OTEL Collector instance
	// until success (or timeout)
	collectorInstance := otelv1alpha1.OpenTelemetryCollector{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "operator-check",
			Namespace: "default",
		},
	}

	// Ensure the collector is not there before the check
	_ = clusterClient.Delete(context.Background(), &collectorInstance)

	fmt.Println("Ensure the creation of OTEL Collectors is available")
	err = wait.Poll(pollInterval, timeoutPoll, func() (done bool, err error) {
		err = clusterClient.Create(
			context.Background(),
			&collectorInstance,
		)
		if err != nil {
			fmt.Println(err)
			return false, nil
		}
		return true, nil
	})

	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	_ = clusterClient.Delete(context.Background(), &collectorInstance)
}
