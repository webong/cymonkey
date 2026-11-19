package contentengine

import (
	"testing"

	"jangolova/contentkit"
)

func TestVideoContentRequiresVideoCapability(t *testing.T) {
	request := contentkit.InterpretRequest{APIVersion: contentkit.ContentAPIVersion, Parts: []contentkit.ContentPart{{Kind: "video", MediaType: "video/mp4", Data: []byte("video")}}}
	if err := contentkit.ValidateInterpretRequest(request); err != nil {
		t.Fatal(err)
	}
	if capability := contentkit.RequiredContentCapability(request.Parts); capability != contentkit.CapabilityVideoInterpret {
		t.Fatalf("required capability = %q", capability)
	}
}
