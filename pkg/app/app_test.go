package app

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	ststypes "github.com/aws/aws-sdk-go-v2/service/sts/types"
	sharedauth "github.com/ffreis/platform-cli/pkg/auth"
	"github.com/spf13/cobra"
)

type fakeSTSClient struct{}

type testContextKey string

const appTestContextKey testContextKey = "key"

const (
	errExitCode1Format = "expected exit code 1, got %d"
	testAWSRegion      = "us-east-1"
)

func (fakeSTSClient) GetCallerIdentity(context.Context, *sts.GetCallerIdentityInput, ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error) {
	return &sts.GetCallerIdentityOutput{Account: sdkaws.String("123456789012"), Arn: sdkaws.String("arn:aws:sts::123456789012:assumed-role/example/session")}, nil
}

func (fakeSTSClient) AssumeRole(context.Context, *sts.AssumeRoleInput, ...func(*sts.Options)) (*sts.AssumeRoleOutput, error) {
	expires := time.Now().Add(time.Hour)
	return &sts.AssumeRoleOutput{Credentials: &ststypes.Credentials{
		AccessKeyId:     sdkaws.String("AKIAASSUMED"),
		SecretAccessKey: sdkaws.String("secret"),
		SessionToken:    sdkaws.String("token"),
		Expiration:      &expires,
	}}, nil
}

func TestExecuteReturnsExitErrorAndWritesMessage(t *testing.T) {
	cmd := &cobra.Command{RunE: func(*cobra.Command, []string) error { return assertErr{} }}
	var stderr bytes.Buffer
	if code := Execute(cmd, &stderr); code != 1 {
		t.Fatalf(errExitCode1Format, code)
	}
	if got := stderr.String(); got != "error: boom\n" {
		t.Fatalf("unexpected stderr: %q", got)
	}
}

func TestNewRootSkipsAWSForLocalCommands(t *testing.T) {
	var profile, region, logLevel, env, org string
	cmd := NewRoot(Options{
		Use:         "example",
		Short:       "example",
		Flags:       FlagBindings{Profile: &profile, Region: &region, LogLevel: &logLevel, Env: &env, Org: &org},
		ValidateEnv: func(string) error { return nil },
		LoadAWSConfig: func(context.Context, string, string) (sdkaws.Config, error) {
			t.Fatal("local command should not load AWS config")
			return sdkaws.Config{}, nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:         "version",
		Annotations: map[string]string{LocalCommandAnnotation: "true"},
		Run: func(*cobra.Command, []string) {
			// Intentionally empty: this test only exercises the local-command auth bypass.
		},
	})
	cmd.SetArgs([]string{"version"})
	if code := Execute(cmd, &bytes.Buffer{}); code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	if region != testAWSRegion || logLevel != "info" || env != "prod" || org != "ffreis" {
		t.Fatalf("unexpected defaults: region=%q logLevel=%q env=%q org=%q", region, logLevel, env, org)
	}
	if profile != "" {
		t.Fatalf("expected empty profile by default, got %q", profile)
	}
}

func TestNewRootRunsAuthFlowAndPublishesRuntime(t *testing.T) {
	var profile, region, logLevel, env, org string
	var captured Runtime
	cmd := NewRoot(Options{
		Use:                   "example",
		Short:                 "example",
		AssumeRoleSessionName: "example-cli",
		Flags:                 FlagBindings{Profile: &profile, Region: &region, LogLevel: &logLevel, Env: &env, Org: &org},
		ValidateEnv:           func(string) error { return nil },
		LoadAWSConfig: func(context.Context, string, string) (sdkaws.Config, error) {
			return sdkaws.Config{Region: testAWSRegion, Credentials: credentials.NewStaticCredentialsProvider("AKIAINIT", "secret", "token")}, nil
		},
		NewSTSClient: func(sdkaws.Config) sharedauth.STSAPI {
			return fakeSTSClient{}
		},
		AfterAuth: func(runtime *Runtime) error {
			captured = *runtime
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{Use: "plan", Run: func(*cobra.Command, []string) {
		// Intentionally empty: this test asserts the pre-run auth flow populates runtime state.
	}})
	cmd.SetArgs([]string{"plan"})
	if code := Execute(cmd, &bytes.Buffer{}); code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	if captured.AccountID != "123456789012" {
		t.Fatalf("unexpected account id: %q", captured.AccountID)
	}
	if captured.CallerARN == "" || captured.Creds.AccessKeyID != "AKIAASSUMED" {
		t.Fatalf("unexpected runtime: %+v", captured)
	}
}

type assertErr struct{}

func (assertErr) Error() string { return "boom" }

func TestResolveRootDependenciesDefaults(t *testing.T) {
	deps := resolveRootDependencies(Options{Use: "example"})
	if deps.annotationKey != LocalCommandAnnotation {
		t.Fatalf("unexpected annotation key: %q", deps.annotationKey)
	}
	if deps.sessionName != "example-cli" {
		t.Fatalf("unexpected session name: %q", deps.sessionName)
	}
	if deps.loadAWSConfig == nil || deps.newSTSClient == nil || deps.assumeRoleFunc == nil {
		t.Fatal("expected default dependencies to be populated")
	}
}

func TestPrepareRootContext(t *testing.T) {
	cmd := &cobra.Command{}
	baseCtx := context.WithValue(context.Background(), appTestContextKey, "base")
	cmd.SetContext(baseCtx)

	validated, err := prepareRootContext(cmd, Options{ValidateEnv: func(string) error { return nil }}, "prod")
	if err != nil || validated != baseCtx {
		t.Fatalf("unexpected validated context result: ctx=%v err=%v", validated, err)
	}

	wantErr := errors.New("bad env")
	_, err = prepareRootContext(cmd, Options{ValidateEnv: func(string) error { return wantErr }}, "prod")
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected validation error, got %v", err)
	}

	replacedCtx := context.WithValue(baseCtx, appTestContextKey, "replaced")
	got, err := prepareRootContext(cmd, Options{BeforeAuth: func(context.Context, *cobra.Command) (context.Context, error) {
		return replacedCtx, nil
	}}, "prod")
	if err != nil || got != replacedCtx || cmd.Context() != replacedCtx {
		t.Fatalf("unexpected replaced context result: ctx=%v cmdCtx=%v err=%v", got, cmd.Context(), err)
	}
}

func TestShouldSkipAuth(t *testing.T) {
	cmd := &cobra.Command{Annotations: map[string]string{LocalCommandAnnotation: "true"}}
	if !shouldSkipAuth(cmd, LocalCommandAnnotation) {
		t.Fatal("expected auth skip for local command")
	}
	if shouldSkipAuth(&cobra.Command{}, LocalCommandAnnotation) {
		t.Fatal("expected auth not to be skipped")
	}
}

func TestEnsureStringPtrWithNil(t *testing.T) {
	// Test that nil input creates a new string pointer
	result := ensureStringPtr(nil)
	if result == nil {
		t.Fatal("expected non-nil pointer from ensureStringPtr(nil)")
	}
	if *result != "" {
		t.Fatalf("expected empty string, got %q", *result)
	}
}

func TestDefaultStringWithEmptyValue(t *testing.T) {
	// Test that empty string returns fallback
	result := defaultString("", "fallback")
	if result != "fallback" {
		t.Fatalf("expected fallback, got %q", result)
	}
}

func TestPrepareRootContextWithBeforeAuthReturningNil(t *testing.T) {
	cmd := &cobra.Command{}
	baseCtx := context.WithValue(context.Background(), appTestContextKey, "base")
	cmd.SetContext(baseCtx)

	// BeforeAuth returns nil for context, should keep original
	got, err := prepareRootContext(cmd, Options{BeforeAuth: func(context.Context, *cobra.Command) (context.Context, error) {
		return nil, nil
	}}, "prod")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != baseCtx {
		t.Fatalf("expected original context when BeforeAuth returns nil")
	}
	if cmd.Context() != baseCtx {
		t.Fatalf("expected cmd context unchanged when BeforeAuth returns nil")
	}
}

func TestBuildPersistentPreRunWithAfterAuthError(t *testing.T) {
	var profile, region, logLevel, env, org string
	wantErr := errors.New("post-auth validation failed")
	cmd := NewRoot(Options{
		Use:         "example",
		Short:       "example",
		Flags:       FlagBindings{Profile: &profile, Region: &region, LogLevel: &logLevel, Env: &env, Org: &org},
		ValidateEnv: func(string) error { return nil },
		LoadAWSConfig: func(context.Context, string, string) (sdkaws.Config, error) {
			return sdkaws.Config{Region: testAWSRegion, Credentials: credentials.NewStaticCredentialsProvider("AKIAINIT", "secret", "token")}, nil
		},
		NewSTSClient: func(sdkaws.Config) sharedauth.STSAPI {
			return fakeSTSClient{}
		},
		AfterAuth: func(*Runtime) error {
			return wantErr
		},
	})
	cmd.AddCommand(&cobra.Command{Use: "plan", Run: func(*cobra.Command, []string) {
		// Intentionally empty: this test only exercises the AfterAuth error path.
	}})
	cmd.SetArgs([]string{"plan"})
	if code := Execute(cmd, &bytes.Buffer{}); code != 1 {
		t.Fatalf(errExitCode1Format, code)
	}
}

func TestDefaultStringWithNonEmptyValue(t *testing.T) {
	// Test that non-empty string returns itself, not fallback
	result := defaultString("production", "fallback")
	if result != "production" {
		t.Fatalf("expected production, got %q", result)
	}
}

func TestPrepareRootContextWithValidateEnvError(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())

	wantErr := errors.New("invalid environment")
	_, err := prepareRootContext(cmd, Options{
		ValidateEnv: func(string) error {
			return wantErr
		},
	}, "staging")
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected validation error, got %v", err)
	}
}

func TestPrepareRootContextWithBeforeAuthError(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())

	wantErr := errors.New("before auth failed")
	_, err := prepareRootContext(cmd, Options{
		BeforeAuth: func(context.Context, *cobra.Command) (context.Context, error) {
			return nil, wantErr
		},
	}, "prod")
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected before-auth error, got %v", err)
	}
}

func TestBuildPersistentPreRunWithAuthenticationError(t *testing.T) {
	var profile, region, logLevel, env, org string
	cmd := NewRoot(Options{
		Use:   "example",
		Short: "example",
		Flags: FlagBindings{Profile: &profile, Region: &region, LogLevel: &logLevel, Env: &env, Org: &org},
		LoadAWSConfig: func(context.Context, string, string) (sdkaws.Config, error) {
			return sdkaws.Config{}, errors.New("failed to load AWS config")
		},
	})
	cmd.AddCommand(&cobra.Command{Use: "apply", Run: func(*cobra.Command, []string) {
		// Intentionally empty: this test only exercises the LoadAWSConfig error path.
	}})
	cmd.SetArgs([]string{"apply"})
	if code := Execute(cmd, &bytes.Buffer{}); code != 1 {
		t.Fatalf(errExitCode1Format, code)
	}
}
