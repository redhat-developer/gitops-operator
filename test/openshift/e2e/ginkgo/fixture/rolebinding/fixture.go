package rolebinding

import (
	"context"
	"reflect"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	matcher "github.com/onsi/gomega/types"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func HaveSubject(subjectParam rbacv1.Subject) matcher.GomegaMatcher {
	return fetchRoleBinding(func(r *rbacv1.RoleBinding) bool {
		for _, subject := range r.Subjects {

			GinkgoWriter.Printf("HaveSubject - ", subject, subjectParam)
			if reflect.DeepEqual(subjectParam, subject) {
				return true
			}

		}
		return false
	})
}

// This is intentionally NOT exported, for now. Create another function in this file/package that calls this function, and export that.
func fetchRoleBinding(f func(*rbacv1.RoleBinding) bool) matcher.GomegaMatcher {

	return WithTransform(func(roleBinding *rbacv1.RoleBinding) bool {

		k8sClient, _, err := utils.GetE2ETestKubeClientWithError()
		if err != nil {
			GinkgoWriter.Println(err)
			return false
		}

		err = k8sClient.Get(context.Background(), client.ObjectKeyFromObject(roleBinding), roleBinding)
		if err != nil {
			GinkgoWriter.Println(err)
			return false
		}

		return f(roleBinding)

	}, BeTrue())

}
