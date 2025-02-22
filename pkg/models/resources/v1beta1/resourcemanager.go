package v1beta1

import (
	"context"
	"strings"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"

	"kubesphere.io/kubesphere/pkg/apiserver/query"

	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const labelResourceServed = "kubesphere.io/resource-served"

// TODO If delete the crd at the cluster when ks is running, the client.cache doesn`t return err but empty result
func New(client client.Client, cache cache.Cache) ResourceManager {
	return &resourceManager{
		client: client,
		cache:  cache,
	}
}

type resourceManager struct {
	client client.Client
	cache  cache.Cache
}

func (h *resourceManager) GetResource(ctx context.Context, gvr schema.GroupVersionResource, namespace, name string) (client.Object, error) {
	var obj client.Object
	gvk, err := h.getGVK(gvr)
	if err != nil {
		return nil, err
	}

	if h.client.Scheme().Recognizes(gvk) {
		gvkObject, err := h.client.Scheme().New(gvk)
		if err != nil {
			return nil, err
		}
		obj = gvkObject.(client.Object)
	} else {
		u := &unstructured.Unstructured{}
		u.SetGroupVersionKind(gvk)
		obj = u
	}

	if err := h.Get(ctx, namespace, name, obj); err != nil {
		return nil, err
	}
	return obj, nil
}

func (h *resourceManager) ListResources(ctx context.Context, gvr schema.GroupVersionResource, namespace string, query *query.Query) (client.ObjectList, error) {
	var obj client.ObjectList

	gvk, err := h.getGVK(gvr)
	if err != nil {
		return nil, err
	}

	gvk = convertGVKToList(gvk)

	if h.client.Scheme().Recognizes(gvk) {
		gvkObject, err := h.client.Scheme().New(gvk)
		if err != nil {
			return nil, err
		}
		obj = gvkObject.(client.ObjectList)
	} else {
		u := &unstructured.UnstructuredList{}
		u.SetGroupVersionKind(gvk)
		obj = u
	}

	if err := h.List(ctx, namespace, query, obj); err != nil {
		return nil, err
	}
	return obj, nil
}

func convertGVKToList(gvk schema.GroupVersionKind) schema.GroupVersionKind {
	if strings.HasSuffix(gvk.Kind, "List") {
		return gvk
	}
	gvk.Kind = gvk.Kind + "List"
	return gvk
}

func (h *resourceManager) getGVK(gvr schema.GroupVersionResource) (schema.GroupVersionKind, error) {
	var (
		gvk schema.GroupVersionKind
		err error
	)
	gvk, err = h.client.RESTMapper().KindFor(gvr)
	if err != nil {
		return gvk, err
	}
	return gvk, nil
}

func (h *resourceManager) IsServed(gvr schema.GroupVersionResource) (bool, error) {
	// well-known group version is already registered
	if h.client.Scheme().IsVersionRegistered(gvr.GroupVersion()) {
		return true, nil
	}

	crd := &extv1.CustomResourceDefinition{}
	if err := h.cache.Get(context.Background(), client.ObjectKey{Name: gvr.GroupResource().String()}, crd); err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}

	if crd.Labels[labelResourceServed] == "true" {
		return true, nil
	}

	return false, nil
}

func (h *resourceManager) Get(ctx context.Context, namespace, name string, object client.Object) error {
	return h.cache.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, object)
}

func (h *resourceManager) List(ctx context.Context, namespace string, query *query.Query, list client.ObjectList) error {
	listOpt := &client.ListOptions{
		LabelSelector: query.Selector(),
		Namespace:     namespace,
	}

	err := h.cache.List(ctx, list, listOpt)
	if err != nil {
		return err
	}

	extractList, err := meta.ExtractList(list)
	if err != nil {
		return err
	}

	filtered, remainingItemCount := DefaultList(extractList, query, compare, filter)
	list.SetRemainingItemCount(remainingItemCount)
	if err := meta.SetList(list, filtered); err != nil {
		return err
	}
	return nil
}

func compare(left, right runtime.Object, field query.Field) bool {
	l, err := meta.Accessor(left)
	if err != nil {
		return false
	}
	r, err := meta.Accessor(right)
	if err != nil {
		return false
	}
	return DefaultObjectMetaCompare(l, r, field)
}

func filter(object runtime.Object, filter query.Filter) bool {
	o, err := meta.Accessor(object)
	if err != nil {
		return false
	}
	return DefaultObjectMetaFilter(o, filter)
}
