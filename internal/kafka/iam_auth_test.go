package kafka

import "testing"

// WO-58: AWS_MSK_IAM is accepted as a valid auth mechanism.
func TestBuildSASLAWSMSKIAM(t *testing.T) {
	_, err := buildSASL(Config{
		AuthMechanism: "AWS_MSK_IAM",
	})
	if err != nil {
		t.Fatalf("buildSASL(AWS_MSK_IAM) error = %v", err)
	}
}

// WO-58: case-insensitive mechanism name.
func TestBuildSASLAWSMSKIAMCaseInsensitive(t *testing.T) {
	_, err := buildSASL(Config{
		AuthMechanism: "aws_msk_iam",
	})
	if err != nil {
		t.Fatalf("buildSASL(aws_msk_iam) error = %v", err)
	}
}
