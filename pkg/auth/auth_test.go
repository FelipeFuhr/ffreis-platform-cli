package auth

import (
	"context"
	"strings"
	"testing"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
)

const testRegion = "us-east-1"

func TestRawCredsToEnv(t *testing.T) {
	creds := RawCreds{AccessKeyID: "AKIA", SecretAccessKey: "secret", SessionToken: "token", Region: testRegion}
	env := creds.ToEnv()
	if env["AWS_ACCESS_KEY_ID"] != "AKIA" || env["AWS_DEFAULT_REGION"] != testRegion {
		t.Fatalf("unexpected env map: %#v", env)
	}
}

func TestLoadAWSConfigRequiresCredentials(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	_, err := LoadAWSConfig(context.Background(), "", testRegion)
	if err == nil || !strings.Contains(err.Error(), "no AWS credentials") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAssumeAdminRoleRejectsRoot(t *testing.T) {
	cfg := sdkaws.Config{Region: testRegion, Credentials: credentials.NewStaticCredentialsProvider("ROOT", "secret", "token")}
	_, _, err := AssumeAdminRole(context.Background(), cfg, "arn:aws:iam::123456789012:root", "123456789012", testRegion, "test-session", nil)
	if err == nil || !strings.Contains(err.Error(), "root AWS credentials are not permitted") {
		t.Fatalf("unexpected error: %v", err)
	}
}
