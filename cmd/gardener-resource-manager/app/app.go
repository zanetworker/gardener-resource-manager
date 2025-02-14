// Copyright (c) 2019 SAP SE or an SAP affiliate company. All rights reserved. This file is licensed under the Apache Software License, v. 2 except as noted otherwise in the LICENSE file
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package app

import (
	"context"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"time"

	resourcesv1alpha1 "github.com/gardener/gardener-resource-manager/pkg/apis/resources/v1alpha1"
	"github.com/gardener/gardener-resource-manager/pkg/controller/managedresources"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var log = logf.Log.WithName("gardener-resource-manager")

// NewControllerManagerCommand creates a new command for running a AWS provider controller.
func NewControllerManagerCommand(ctx context.Context) *cobra.Command {
	logf.SetLogger(logf.ZapLogger(false))
	entryLog := log.WithName("entrypoint")

	var (
		leaderElection          bool
		leaderElectionNamespace string
		syncPeriod              time.Duration
		targetKubeconfigPath    string
		maxConcurrentWorkers    int
		namespace               string
	)

	cmd := &cobra.Command{
		Use: "gardener-resource-manager",

		Run: func(cmd *cobra.Command, args []string) {
			mgr, err := manager.New(config.GetConfigOrDie(), manager.Options{
				LeaderElection:          leaderElection,
				LeaderElectionID:        "gardener-resource-manager",
				LeaderElectionNamespace: leaderElectionNamespace,
				SyncPeriod:              &syncPeriod,
				Namespace:               namespace,
			})
			if err != nil {
				entryLog.Error(err, "could not instantiate manager")
				os.Exit(1)
			}

			utilruntime.Must(resourcesv1alpha1.AddToScheme(mgr.GetScheme()))

			targetClient, err := getTargetClient(targetKubeconfigPath)
			if err != nil {
				entryLog.Error(err, "unable to create client for target cluster")
				os.Exit(1)
			}

			c, err := controller.New("resource-controller", mgr, controller.Options{
				MaxConcurrentReconciles: maxConcurrentWorkers,
				Reconciler: managedresources.NewReconciler(
					ctx,
					log.WithName("reconciler"),
					mgr.GetScheme(),
					mgr.GetClient(),
					targetClient,
				),
			})
			if err != nil {
				entryLog.Error(err, "unable to set up individual controller")
				os.Exit(1)
			}

			if err := c.Watch(
				&source.Kind{Type: &resourcesv1alpha1.ManagedResource{}},
				&handler.EnqueueRequestForObject{},
				managedresources.GenerationChangedPredicate(),
			); err != nil {
				entryLog.Error(err, "unable to watch ManagedResources")
				os.Exit(1)
			}
			if err := c.Watch(
				&source.Kind{Type: &corev1.Secret{}},
				&handler.EnqueueRequestsFromMapFunc{ToRequests: managedresources.SecretToManagedResourceMapper(mgr.GetClient(), nil)},
			); err != nil {
				entryLog.Error(err, "unable to watch Secrets mapping to ManagedResources")
				os.Exit(1)
			}

			entryLog.Info("Managed namespace: " + namespace)
			entryLog.Info("Sync period: " + syncPeriod.String())

			if err := mgr.Start(ctx.Done()); err != nil {
				entryLog.Error(err, "error running manager")
				os.Exit(1)
			}
		},
	}

	cmd.Flags().BoolVar(&leaderElection, "leader-election", true, "enable or disable leader election")
	cmd.Flags().StringVar(&leaderElectionNamespace, "leader-election-namespace", "", "namespace for leader election")
	cmd.Flags().DurationVar(&syncPeriod, "sync-period", time.Minute, "duration how often existing resources should be synced")
	cmd.Flags().StringVar(&targetKubeconfigPath, "target-kubeconfig", "", "path to the kubeconfig for the target cluster")
	cmd.Flags().IntVar(&maxConcurrentWorkers, "max-concurrent-workers", 10, "number of worker threads for concurrent reconciliation of resources")
	cmd.Flags().StringVar(&namespace, "namespace", "", "namespace in which the ManagedResources should be observed (defaults to all namespaces)")

	return cmd
}

func getTargetConfig(kubeconfigPath string) (*rest.Config, error) {
	if len(kubeconfigPath) > 0 {
		return clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	}
	if kubeconfig := os.Getenv("KUBECONFIG"); len(kubeconfig) > 0 {
		return clientcmd.BuildConfigFromFlags("", kubeconfig)
	}
	if c, err := rest.InClusterConfig(); err == nil {
		return c, nil
	}
	if usr, err := user.Current(); err == nil {
		if c, err := clientcmd.BuildConfigFromFlags("", filepath.Join(usr.HomeDir, ".kube", "config")); err == nil {
			return c, nil
		}
	}
	return nil, fmt.Errorf("could not create config for cluster")
}

func getTargetClient(kubeconfigPath string) (client.Client, error) {
	targetConfig, err := getTargetConfig(kubeconfigPath)
	if err != nil {
		return nil, err
	}

	targetConfig.QPS = 100.0
	targetConfig.Burst = 130

	return client.New(targetConfig, client.Options{})
}
