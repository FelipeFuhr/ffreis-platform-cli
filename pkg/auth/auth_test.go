package auth

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	ststypes "github.com/aws/aws-sdk-go-v2/service/sts/types"
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

func TestAssumeAdminRoleSkipsAssumeWhenAlreadyPlatformAdmin(t *testing.T) {
	cfg := sdkaws.Config{Region: testRegion, Credentials: credentials.NewStaticCredentialsProvider("AKIA", "secret", "token")}
	returnedCfg, creds, err := AssumeAdminRole(context.Background(), cfg, "arn:aws:iam::123456789012:assumed-role/platform-admin/user", "123456789012", testRegion, "test-session", nil)
	if err != nil {
		t.Fatalf("AssumeAdminRole() error = %v", err)
	}
	if creds.AccessKeyID != "AKIA" || creds.SecretAccessKey != "secret" {
		t.Fatalf("unexpected creds: %+v", creds)
	}
	// Verify region is set correctly on returned config
	if returnedCfg.Region != testRegion {
		t.Fatalf("expected same region in returned config")
	}
}

func TestAssumeAdminRoleCallsSTS(t *testing.T) {
	cfg := sdkaws.Config{Region: testRegion, Credentials: credentials.NewStaticCredentialsProvider("AKIA", "secret", "token")}
	var assumeRoleCalled bool
	mockSTS := func(cfg sdkaws.Config) STSAPI {
		return &mockSTSClient{
			assumeRoleFn: func(ctx context.Context, in *sts.AssumeRoleInput, optFns ...func(*sts.Options)) (*sts.AssumeRoleOutput, error) {
				assumeRoleCalled = true
				return &sts.AssumeRoleOutput{
					Credentials: &ststypes.Credentials{
						AccessKeyId:     sdkaws.String("ASSUMED_AKIA"),
						SecretAccessKey: sdkaws.String("assumed_secret"),
						SessionToken:    sdkaws.String("assumed_token"),
					},
				}, nil
			},
		}
	}
	returnedCfg, creds, err := AssumeAdminRole(context.Background(), cfg, "arn:aws:iam::123456789012:user/service", "123456789012", testRegion, "test-session", mockSTS)
	if err != nil {
		t.Fatalf("AssumeAdminRole() error = %v", err)
	}
	if !assumeRoleCalled {
		t.Fatal("AssumeRole should have been called")
	}
	if creds.AccessKeyID != "ASSUMED_AKIA" || creds.SessionToken != "assumed_token" {
		t.Fatalf("unexpected assumed creds: %+v", creds)
	}
	if returnedCfg.Region != testRegion {
		t.Fatalf("unexpected returned config region: %s", returnedCfg.Region)
	}
}

func TestNewLoggerLevels(t *testing.T) {
	tests := []struct {
		level     string
		wantLevel slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"DEBUG", slog.LevelDebug},
		{"warn", slog.LevelWarn},
		{"WARN", slog.LevelWarn},
		{"error", slog.LevelError},
		{"ERROR", slog.LevelError},
		{"info", slog.LevelInfo},
		{"INFO", slog.LevelInfo},
		{"unknown", slog.LevelInfo}, // defaults to info
		{"", slog.LevelInfo},        // defaults to info
	}
	for _, tt := range tests {
		t.Run(tt.level, func(t *testing.T) {
			logger := NewLogger(tt.level)
			if logger == nil {
				t.Fatal("NewLogger returned nil")
			}
			if logger.Enabled(context.Background(), tt.wantLevel) == false {
				// Logger should be enabled for its level
				t.Errorf("logger not enabled for level %v", tt.wantLevel)
			}
		})
	}
}

func TestRawCredsToEnvCompleteness(t *testing.T) {
	tests := []struct {
		name  string
		creds RawCreds
		want  map[string]string
	}{
		{
			"full creds",
			RawCreds{AccessKeyID: "AKIA123", SecretAccessKey: "secret", SessionToken: "token", Region: "us-west-2"},
			map[string]string{
				"AWS_ACCESS_KEY_ID":     "AKIA123",
				"AWS_SECRET_ACCESS_KEY": "secret",
				"AWS_SESSION_TOKEN":     "token",
				"AWS_REGION":            "us-west-2",
				"AWS_DEFAULT_REGION":    "us-west-2",
			},
		},
		{
			"no session token",
			RawCreds{AccessKeyID: "AKIA123", SecretAccessKey: "secret", Region: "eu-central-1"},
			map[string]string{
				"AWS_ACCESS_KEY_ID":     "AKIA123",
				"AWS_SECRET_ACCESS_KEY": "secret",
				"AWS_SESSION_TOKEN":     "",
				"AWS_REGION":            "eu-central-1",
				"AWS_DEFAULT_REGION":    "eu-central-1",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.creds.ToEnv()
			for key, want := range tt.want {
				if got[key] != want {
					t.Errorf("ToEnv()[%s] = %q, want %q", key, got[key], want)
				}
			}
			if len(got) != len(tt.want) {
				t.Errorf("ToEnv() returned %d keys, want %d", len(got), len(tt.want))
			}
		})
	}
}

func TestLoadAWSConfigWithProfile(t *testing.T) {
	// This test verifies the function handles the profile case
	// Note: Full integration test would require AWS credentials/config
	ctx := context.Background()
	// Test with empty profile and no env creds should error
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	_, err := LoadAWSConfig(ctx, "", testRegion)
	if err == nil {
		t.Fatal("expected error when no creds available")
	}
}

// mockSTSClient implements STSAPI for testing
type mockSTSClient struct {
	getCallerIdentityFn func(context.Context, *sts.GetCallerIdentityInput, ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error)
	assumeRoleFn        func(context.Context, *sts.AssumeRoleInput, ...func(*sts.Options)) (*sts.AssumeRoleOutput, error)
}

func (m *mockSTSClient) GetCallerIdentity(ctx context.Context, in *sts.GetCallerIdentityInput, optFns ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error) {
	if m.getCallerIdentityFn != nil {
		return m.getCallerIdentityFn(ctx, in, optFns...)
	}
	return nil, errors.New("not implemented")
}

func (m *mockSTSClient) AssumeRole(ctx context.Context, in *sts.AssumeRoleInput, optFns ...func(*sts.Options)) (*sts.AssumeRoleOutput, error) {
	if m.assumeRoleFn != nil {
		return m.assumeRoleFn(ctx, in, optFns...)
	}
	return nil, errors.New("not implemented")
}
