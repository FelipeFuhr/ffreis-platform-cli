package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	sdkcfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

type STSAPI interface {
	GetCallerIdentity(context.Context, *sts.GetCallerIdentityInput, ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error)
	AssumeRole(context.Context, *sts.AssumeRoleInput, ...func(*sts.Options)) (*sts.AssumeRoleOutput, error)
}

type RawCreds struct {
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	Region          string
}

func (c RawCreds) ToEnv() map[string]string {
	return map[string]string{
		"AWS_ACCESS_KEY_ID":     c.AccessKeyID,
		"AWS_SECRET_ACCESS_KEY": c.SecretAccessKey,
		"AWS_SESSION_TOKEN":     c.SessionToken,
		"AWS_REGION":            c.Region,
		"AWS_DEFAULT_REGION":    c.Region,
	}
}

func LoadAWSConfig(ctx context.Context, profile, region string) (sdkaws.Config, error) {
	opts := []func(*sdkcfg.LoadOptions) error{sdkcfg.WithRegion(region)}
	switch {
	case profile != "":
		opts = append(opts, sdkcfg.WithSharedConfigProfile(profile))
	case os.Getenv("AWS_ACCESS_KEY_ID") != "":
	default:
		return sdkaws.Config{}, errors.New("no AWS credentials: set --profile or AWS_ACCESS_KEY_ID/AWS_SECRET_ACCESS_KEY")
	}
	cfg, err := sdkcfg.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return sdkaws.Config{}, fmt.Errorf("loading AWS config: %w", err)
	}
	return cfg, nil
}

func DefaultSTSClient(cfg sdkaws.Config) STSAPI {
	return sts.NewFromConfig(cfg)
}

func AssumeAdminRole(ctx context.Context, cfg sdkaws.Config, callerARN, accountID, region, sessionName string, newSTSClient func(sdkaws.Config) STSAPI) (sdkaws.Config, RawCreds, error) {
	if newSTSClient == nil {
		newSTSClient = DefaultSTSClient
	}

	initCreds, err := cfg.Credentials.Retrieve(ctx)
	if err != nil {
		return sdkaws.Config{}, RawCreds{}, fmt.Errorf("retrieving initial credentials: %w", err)
	}
	initial := RawCreds{
		AccessKeyID:     initCreds.AccessKeyID,
		SecretAccessKey: initCreds.SecretAccessKey,
		SessionToken:    initCreds.SessionToken,
		Region:          region,
	}

	if strings.Contains(callerARN, "assumed-role/platform-admin/") {
		return cfg, initial, nil
	}

	if strings.HasSuffix(callerARN, ":root") {
		return sdkaws.Config{}, RawCreds{}, errors.New("root AWS credentials are not permitted in downstream project repos; use a named profile or principal that can assume platform-admin")
	}

	roleARN := fmt.Sprintf("arn:aws:iam::%s:role/platform-admin", accountID)
	stsc := newSTSClient(cfg)
	out, err := stsc.AssumeRole(ctx, &sts.AssumeRoleInput{
		RoleArn:         sdkaws.String(roleARN),
		RoleSessionName: sdkaws.String(sessionName),
		DurationSeconds: sdkaws.Int32(3600),
	})
	if err != nil {
		return sdkaws.Config{}, RawCreds{}, fmt.Errorf("assuming platform-admin role: %w", err)
	}

	rc := RawCreds{
		AccessKeyID:     sdkaws.ToString(out.Credentials.AccessKeyId),
		SecretAccessKey: sdkaws.ToString(out.Credentials.SecretAccessKey),
		SessionToken:    sdkaws.ToString(out.Credentials.SessionToken),
		Region:          region,
	}
	assumedCfg, err := sdkcfg.LoadDefaultConfig(ctx,
		sdkcfg.WithRegion(region),
		sdkcfg.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(rc.AccessKeyID, rc.SecretAccessKey, rc.SessionToken)),
	)
	if err != nil {
		return sdkaws.Config{}, RawCreds{}, fmt.Errorf("building assumed-role config: %w", err)
	}
	return assumedCfg, rc, nil
}

func NewLogger(level string) *slog.Logger {
	var l slog.Level
	switch strings.ToLower(level) {
	case "debug":
		l = slog.LevelDebug
	case "warn":
		l = slog.LevelWarn
	case "error":
		l = slog.LevelError
	default:
		l = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: l}))
}
