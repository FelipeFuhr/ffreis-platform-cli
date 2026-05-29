package app

import (
	"context"
	"fmt"
	"io"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/spf13/cobra"

	sharedauth "github.com/ffreis/platform-cli/pkg/auth"
)

const LocalCommandAnnotation = "local"

type FlagBindings struct {
	Profile  *string
	Region   *string
	LogLevel *string
	Env      *string
	Org      *string
}

type Runtime struct {
	Profile   string
	Region    string
	LogLevel  string
	Env       string
	Org       string
	AccountID string
	CallerARN string
	Creds     sharedauth.RawCreds
	AWSConfig sdkaws.Config
}

type AssumeAdminRoleFunc func(context.Context, sdkaws.Config, string, string, string, string, func(sdkaws.Config) sharedauth.STSAPI) (sdkaws.Config, sharedauth.RawCreds, error)

type Options struct {
	Use                   string
	Short                 string
	DefaultRegion         string
	DefaultLogLevel       string
	DefaultEnv            string
	DefaultOrg            string
	AssumeRoleSessionName string
	Flags                 FlagBindings
	ValidateEnv           func(string) error
	BeforeAuth            func(context.Context, *cobra.Command) (context.Context, error)
	AfterAuth             func(*Runtime) error
	LoadAWSConfig         func(context.Context, string, string) (sdkaws.Config, error)
	NewSTSClient          func(sdkaws.Config) sharedauth.STSAPI
	AssumeAdminRole       AssumeAdminRoleFunc
	LocalCommandKey       string
}

type rootFlagValues struct {
	profile  *string
	region   *string
	logLevel *string
	env      *string
	org      *string
}

type rootDependencies struct {
	annotationKey  string
	sessionName    string
	loadAWSConfig  func(context.Context, string, string) (sdkaws.Config, error)
	newSTSClient   func(sdkaws.Config) sharedauth.STSAPI
	assumeRoleFunc AssumeAdminRoleFunc
}

func NewRoot(opts Options) *cobra.Command {
	values := newRootFlagValues(opts.Flags)
	deps := resolveRootDependencies(opts)

	cmd := &cobra.Command{
		Use:               opts.Use,
		Short:             opts.Short,
		SilenceErrors:     true,
		SilenceUsage:      true,
		PersistentPreRunE: buildPersistentPreRun(opts, values, deps),
	}

	bindPersistentFlags(cmd, opts, values)

	return cmd
}

func Execute(cmd *cobra.Command, stderr io.Writer) int {
	if err := cmd.Execute(); err != nil {
		if message := err.Error(); message != "" {
			_, _ = io.WriteString(stderr, "error: "+message+"\n")
		}
		return 1
	}
	return 0
}

func ensureStringPtr(value *string) *string {
	if value != nil {
		return value
	}
	return new(string)
}

func defaultString(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func newRootFlagValues(flags FlagBindings) rootFlagValues {
	return rootFlagValues{
		profile:  ensureStringPtr(flags.Profile),
		region:   ensureStringPtr(flags.Region),
		logLevel: ensureStringPtr(flags.LogLevel),
		env:      ensureStringPtr(flags.Env),
		org:      ensureStringPtr(flags.Org),
	}
}

func resolveRootDependencies(opts Options) rootDependencies {
	deps := rootDependencies{
		annotationKey:  opts.LocalCommandKey,
		sessionName:    opts.AssumeRoleSessionName,
		loadAWSConfig:  opts.LoadAWSConfig,
		newSTSClient:   opts.NewSTSClient,
		assumeRoleFunc: opts.AssumeAdminRole,
	}
	if deps.annotationKey == "" {
		deps.annotationKey = LocalCommandAnnotation
	}
	if deps.loadAWSConfig == nil {
		deps.loadAWSConfig = sharedauth.LoadAWSConfig
	}
	if deps.newSTSClient == nil {
		deps.newSTSClient = sharedauth.DefaultSTSClient
	}
	if deps.assumeRoleFunc == nil {
		deps.assumeRoleFunc = sharedauth.AssumeAdminRole
	}
	if deps.sessionName == "" {
		deps.sessionName = opts.Use + "-cli"
	}
	return deps
}

func buildPersistentPreRun(opts Options, values rootFlagValues, deps rootDependencies) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		ctx, err := prepareRootContext(cmd, opts, *values.env)
		if err != nil {
			return err
		}
		if shouldSkipAuth(cmd, deps.annotationKey) {
			return nil
		}

		runtime, err := authenticateRuntime(ctx, values, deps)
		if err != nil {
			return err
		}
		if opts.AfterAuth == nil {
			return nil
		}
		return opts.AfterAuth(runtime)
	}
}

func prepareRootContext(cmd *cobra.Command, opts Options, env string) (context.Context, error) {
	ctx := cmd.Context()
	if opts.ValidateEnv != nil {
		if err := opts.ValidateEnv(env); err != nil {
			return nil, err
		}
	}
	if opts.BeforeAuth == nil {
		return ctx, nil
	}
	nextCtx, err := opts.BeforeAuth(ctx, cmd)
	if err != nil {
		return nil, err
	}
	if nextCtx != nil {
		ctx = nextCtx
		cmd.SetContext(ctx)
	}
	return ctx, nil
}

func shouldSkipAuth(cmd *cobra.Command, annotationKey string) bool {
	return cmd.Annotations[annotationKey] == "true"
}

func authenticateRuntime(ctx context.Context, values rootFlagValues, deps rootDependencies) (*Runtime, error) {
	awsCfg, err := deps.loadAWSConfig(ctx, *values.profile, *values.region)
	if err != nil {
		return nil, err
	}

	stsClient := deps.newSTSClient(awsCfg)
	identity, err := stsClient.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return nil, fmt.Errorf("verifying AWS credentials: %w", err)
	}

	accountID := sdkaws.ToString(identity.Account)
	callerARN := sdkaws.ToString(identity.Arn)
	assumedCfg, assumedCreds, err := deps.assumeRoleFunc(ctx, awsCfg, callerARN, accountID, *values.region, deps.sessionName, deps.newSTSClient)
	if err != nil {
		return nil, err
	}

	return &Runtime{
		Profile:   *values.profile,
		Region:    *values.region,
		LogLevel:  *values.logLevel,
		Env:       *values.env,
		Org:       *values.org,
		AccountID: accountID,
		CallerARN: callerARN,
		Creds:     assumedCreds,
		AWSConfig: assumedCfg,
	}, nil
}

func bindPersistentFlags(cmd *cobra.Command, opts Options, values rootFlagValues) {
	cmd.PersistentFlags().StringVar(values.profile, "profile", "", "AWS named profile (or use AWS_ACCESS_KEY_ID env vars)")
	cmd.PersistentFlags().StringVar(values.region, "region", defaultString(opts.DefaultRegion, "us-east-1"), "AWS region")
	cmd.PersistentFlags().StringVar(values.logLevel, "log-level", defaultString(opts.DefaultLogLevel, "info"), "Log level: debug, info, warn, error")
	cmd.PersistentFlags().StringVar(values.env, "env", defaultString(opts.DefaultEnv, "prod"), "Environment")
	cmd.PersistentFlags().StringVar(values.org, "org", defaultString(opts.DefaultOrg, "ffreis"), "Organisation name (used to construct resource names)")
}
