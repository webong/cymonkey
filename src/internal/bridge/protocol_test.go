package bridge

import "testing"

func TestProtocolIdentity(t *testing.T) {
	t.Parallel()

	if ProtocolVersion != "jangolova.bridge/v1alpha1" {
		t.Fatalf("ProtocolVersion = %q", ProtocolVersion)
	}
	for name, method := range map[string]string{
		"hello":        MethodHello,
		"capabilities": MethodCapabilities,
		"describe":     MethodDescribe,
		"act":          MethodAct,
		"events":       MethodEvents,
	} {
		if method != name {
			t.Fatalf("%s method = %q", name, method)
		}
	}
}
