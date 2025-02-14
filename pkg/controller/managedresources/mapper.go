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

package managedresources

import (
	"context"

	resourcesv1alpha1 "github.com/gardener/gardener-resource-manager/pkg/apis/resources/v1alpha1"

	extensionscontroller "github.com/gardener/gardener-extensions/pkg/controller"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type secretToManagedResourceMapper struct {
	client     client.Client
	predicates []predicate.Predicate
}

func (m *secretToManagedResourceMapper) Map(obj handler.MapObject) []reconcile.Request {
	if obj.Object == nil {
		return nil
	}

	secret, ok := obj.Object.(*corev1.Secret)
	if !ok {
		return nil
	}

	managedResourceList := &resourcesv1alpha1.ManagedResourceList{}
	if err := m.client.List(context.TODO(), client.InNamespace(secret.Namespace), managedResourceList); err != nil {
		return nil
	}

	var requests []reconcile.Request
	for _, mr := range managedResourceList.Items {
		if !extensionscontroller.EvalGenericPredicate(&mr, m.predicates...) {
			continue
		}

		for _, secretRef := range mr.Spec.SecretRefs {
			if secretRef.Name == secret.Name {
				requests = append(requests, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Namespace: mr.Namespace,
						Name:      mr.Name,
					},
				})
			}
		}
	}
	return requests
}

// SecretToManagedResourceMapper returns a mapper that returns requests for ManagedResources whose
// referenced secrets have been modified.
func SecretToManagedResourceMapper(client client.Client, predicates []predicate.Predicate) handler.Mapper {
	return &secretToManagedResourceMapper{client, predicates}
}
