package common

import "testing"

func TestNamespaceNodeSelectorAnnotation(t *testing.T) {
	tests := map[string]struct {
		runOnInfra   bool
		nodeSelector map[string]string
		want         string
	}{
		"neither": {
			want: "",
		},
		"runOnInfra only keeps exact infra value": {
			runOnInfra: true,
			want:       InfraNodeSelectorAnnotationValue,
		},
		"custom selector": {
			nodeSelector: map[string]string{"foo": "bar"},
			want:         "foo=bar",
		},
		"runOnInfra plus custom merges": {
			runOnInfra:   true,
			nodeSelector: map[string]string{"foo": "bar"},
			want:         "foo=bar,node-role.kubernetes.io/infra=",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := NamespaceNodeSelectorAnnotation(tt.runOnInfra, tt.nodeSelector)
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}
