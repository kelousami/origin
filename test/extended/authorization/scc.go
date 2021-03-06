package authorization

import (
	"context"
	"fmt"
	"strings"

	g "github.com/onsi/ginkgo"
	"github.com/openshift/origin/pkg/test/ginkgo/result"
	exutil "github.com/openshift/origin/test/extended/util"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = g.Describe("[sig-auth][Feature:SCC][Early]", func() {
	defer g.GinkgoRecover()

	oc := exutil.NewCLI("working-scc-during-install")

	g.It("should not have pod creation failures during install", func() {
		kubeClient := oc.AdminKubeClient()

		events, err := kubeClient.CoreV1().Events("").List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			g.Fail(fmt.Sprintf("Unexpected error: %v", err))
		}

		denialStrings := []string{}
		for _, event := range events.Items {
			if !strings.Contains(event.Message, "unable to validate against any security context constraint") {
				continue
			}
			// TODO if we need more details, this is a good guess.
			//eventBytes, err := json.Marshal(event)
			//if err != nil {
			//	e2e.Logf("%v", spew.Sdump(event))
			//} else {
			//	e2e.Logf("%v", string(eventBytes))
			//}
			// try with a short summary we can actually read first
			denialString := fmt.Sprintf("%v for %v.%v/%v -n %v happened %d times", event.Message, event.InvolvedObject.Kind, event.InvolvedObject.APIVersion, event.InvolvedObject.Name, event.InvolvedObject.Namespace, event.Count)
			denialStrings = append(denialStrings, denialString)
		}
		result.Flakef(strings.Join(denialStrings, "\n"))
	})
})
