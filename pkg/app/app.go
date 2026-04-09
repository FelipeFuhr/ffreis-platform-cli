package app

import (
	"context"
	"fmt"
	"io"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	sharedauth "github.com/ffreis/platform-cli/pkg/auth"
	"github.com/spf13/cobra"
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

func NewRoot(opts Options) *cobra.Command {
	profile := ensureStringPtr(opts.Flags.Profile)
	region := ensureStringPtr(opts.Flags.Region)
	logLevel := ensureStringPtr(opts.Flags.LogLevel)
	env := ensureStringPtr(opts.Flags.Env)
	org := ensureStringPtr(opts.Flags.Org)

	annotationKey := opts.LocalCommandKey
	if annotationKey == "" {
		annotationKey = LocalCommandAnnotation
	}
	loadAWSConfig := opts.LoadAWSConfig
	if loadAWSConfig == nil {
		loadAWSConfig = sharedauth.LoadAWSConfig
	}
	newSTSClient := opts.NewSTSClient
	if newSTSClient == nil {
		newSTSClient = sharedauth.DefaultSTSClient
	}
	assumeAdminRole := opts.AssumeAdminRole
	if assumeAdminRole == nil {
		assumeAdminRole = sharedauth.AssumeAdminRole
	}
	sessionName := opts.AssumeRoleSessionName
	if sessionName == "" {
		sessionName = opts.Use + "-cli"
	}

	cmd := &cobra.Command{
		Use:           opts.Use,
		Short:         opts.Short,
		SilenceErrors: true,
		SilenceUsage:  true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()

			if opts.ValidateEnv != nil {
				if err := opts.ValidateEnv(*env); err != nil {
					return err
				}
			}

			if opts.BeforeAuth != nil {
				nextCtx, err := opts.BeforeAuth(ctx, cmd)
				if err != nil {
					return err
				}
				if nextCtx != nil {
					ctx = nextCtx
					cmd.SetContext(ctx)
				}
			}

			if cmd.Annotations[annotationKey] == "true" {
				return nil
			}

			awsCfg, err := loadAWSConfig(ctx, *profile, *region)
			if err != nil {
				return err
			}

			stsClient := newSTSClient(awsCfg)
			identity, err := stsClient.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
			if err != nil {
				return fmt.Errorf("verifying AWS credentials: %w", err)
			}

			accountID := sdkaws.ToString(identity.Account)
			callerARN := sdkaws.ToString(identity.Arn)
			assumedCfg, assumedCreds, err := assumeAdminRole(ctx, awsCfg, callerARN, accountID, *region, sessionName, newSTSClient)
			if err != nil {
				return err
			}

			if opts.AfterAuth != nil {
				return opts.AfterAuth(&Runtime{
					Profile:   *profile,
					Region:    *region,
					LogLevel:  *logLevel,
					Env:       *env,
					Org:       *org,
					AccountID: accountID,
					CallerARN: callerARN,
					Creds:     assumedCreds,
					AWSConfig: assumedCfg,
				})
			}

			return nil
		},
	}

	cmd.PersistentFlags().StringVar(profile, "profile", "", "AWS named profile (or use AWS_ACCESS_KEY_ID env vars)")
	cmd.PersistentFlags().StringVar(region, "region", defaultString(opts.DefaultRegion, "us-east-1"), "AWS region")
	cmd.PersistentFlags().StringVar(logLevel, "log-level", defaultString(opts.DefaultLogLevel, "info"), "Log level: debug, info, warn, error")
	cmd.PersistentFlags().StringVar(env, "env", defaultString(opts.DefaultEnv, "prod"), "Environment")
	cmd.PersistentFlags().StringVar(org, "org", defaultString(opts.DefaultOrg, "ffreis"), "Organisation name (used to construct resource names)")

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
