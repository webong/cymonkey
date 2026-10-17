package contentkit

import "testing"

func TestVideoContentRequiresVideoCapability(t *testing.T) {
	request := InterpretRequest{APIVersion: ContentAPIVersion, Parts: []ContentPart{{Kind: "video", MediaType: "video/mp4", Data: []byte("video")}}}
	if err := ValidateInterpretRequest(request); err != nil {
		t.Fatal(err)
	}
	if capability := requiredContentCapability(request.Parts); capability != CapabilityVideoInterpret {
		t.Fatalf("required capability = %q", capability)
	}
}
