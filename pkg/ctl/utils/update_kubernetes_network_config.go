package utils

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"

	"github.com/kris-nova/logger"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	api "github.com/weaveworks/eksctl/pkg/apis/eksctl.io/v1alpha5"
	"github.com/weaveworks/eksctl/pkg/ctl/cmdutils"
)

func updateKubernetesNetworkConfig(cmd *cmdutils.Cmd, handler func(*cmdutils.Cmd) error) {
	cfg := api.NewClusterConfig()
	cmd.ClusterConfig = cfg

	cmd.SetDescription("update-kubernetes-network-config", "update Kubernetes network config", "update the Kubernetes network config on a cluster")

	var enableELB bool
	cmd.CobraCommand.RunE = func(_ *cobra.Command, args []string) error {
		cmd.NameArg = cmdutils.GetNameArg(args)
		if err := cmdutils.NewKubernetesNetworkConfigLoader(cmd).Load(); err != nil {
			return err
		}
		if cmd.ClusterConfigFile == "" {
			cfg.KubernetesNetworkConfig = &api.KubernetesNetworkConfig{
				ElasticLoadBalancing: &api.ElasticLoadBalancing{
					Enabled: &enableELB,
				},
			}
		}
		return handler(cmd)
	}

	cmdutils.AddCommonFlagsForAWS(cmd, &cmd.ProviderConfig, false)

	cmd.FlagSetGroup.InFlagSet("General", func(fs *pflag.FlagSet) {
		cmdutils.AddClusterFlag(fs, cfg.Metadata)
		cmdutils.AddRegionFlag(fs, &cmd.ProviderConfig)
		cmdutils.AddConfigFileFlag(fs, &cmd.ClusterConfigFile)
		fs.BoolVar(&enableELB, "enable-elastic-load-balancing", true, "Enable the load balancing capability on a cluster")
	})
}

func updateKubernetesNetworkConfigCmd(cmd *cmdutils.Cmd) {
	updateKubernetesNetworkConfig(cmd, doUpdateKubernetesNetworkConfig)
}

func doUpdateKubernetesNetworkConfig(cmd *cmdutils.Cmd) error {
	cfg := cmd.ClusterConfig
	ctx := context.Background()
	if cfg.Metadata.Name == "" {
		return cmdutils.ErrMustBeSet(cmdutils.ClusterNameFlag(cmd))
	}
	// capture the requested state first, as NewProviderForExistingCluster overwrites kubernetesNetworkConfig
	elb := cfg.ElasticLoadBalancingRequest()
	ctl, err := cmd.NewProviderForExistingCluster(ctx)
	if err != nil {
		return err
	}
	makeELBStatus := func(enabled *bool) string {
		if api.IsEnabled(enabled) {
			return "enabled"
		}
		return "disabled"
	}
	if knc := ctl.Status.ClusterInfo.Cluster.KubernetesNetworkConfig; knc != nil && elasticLoadBalancingUpToDate(knc.ElasticLoadBalancing, elb) {
		logger.Info("the load balancing capability is already %s", makeELBStatus(elb.Enabled))
		return nil
	}
	if err := ctl.UpdateClusterConfig(ctx, &eks.UpdateClusterConfigInput{
		Name: aws.String(cfg.Metadata.Name),
		KubernetesNetworkConfig: &ekstypes.KubernetesNetworkConfigRequest{
			ElasticLoadBalancing: elb,
		},
	}); err != nil {
		return fmt.Errorf("updating Kubernetes network config: %w", err)
	}
	logger.Info("the load balancing capability was %s successfully", makeELBStatus(elb.Enabled))
	return nil
}

// elasticLoadBalancingUpToDate reports whether the current config already matches the request.
// An unset GatewayApi in the request leaves the current setting unchanged, but a requested GatewayApi
// only matches if the cluster has one, so that `managed: false` still enables it on a cluster without one.
func elasticLoadBalancingUpToDate(current, requested *ekstypes.ElasticLoadBalancing) bool {
	if current == nil || aws.ToBool(current.Enabled) != aws.ToBool(requested.Enabled) {
		return false
	}
	if requested.GatewayApi == nil {
		return true
	}
	if current.GatewayApi == nil || current.GatewayApi.Crds == nil {
		return false
	}
	return aws.ToBool(current.GatewayApi.Crds.Managed) == aws.ToBool(requested.GatewayApi.Crds.Managed)
}
