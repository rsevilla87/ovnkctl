package cmd

import (
	"fmt"
	"os"

	"github.com/rsevilla/ovnkctl/cmd/inspect"
	"github.com/rsevilla/ovnkctl/cmd/show"
	"github.com/rsevilla/ovnkctl/cmd/trace"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

var (
	kubeConfigFlags *genericclioptions.ConfigFlags
	outputFormat    string
)

var rootCmd = &cobra.Command{
	Use:   "ovnkctl",
	Short: "OVN-Kubernetes diagnostic and inspection tool",
	Long:  "ovnkctl simplifies the management and troubleshooting of OVN-Kubernetes networking by providing unified access to OVN and Kubernetes state.",
}

func init() {
	kubeConfigFlags = genericclioptions.NewConfigFlags(true)
	kubeConfigFlags.AddFlags(rootCmd.PersistentFlags())
	rootCmd.PersistentFlags().StringVarP(&outputFormat, "output", "o", "table", "Output format: table, json, yaml")

	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(show.NewShowCmd(kubeConfigFlags, &outputFormat))
	rootCmd.AddCommand(inspect.NewInspectCmd(kubeConfigFlags, &outputFormat))
	rootCmd.AddCommand(trace.NewTraceCmd(kubeConfigFlags))
}

func Execute() error {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return err
	}
	return nil
}

func GetKubeConfigFlags() *genericclioptions.ConfigFlags {
	return kubeConfigFlags
}

func GetOutputFormat() string {
	return outputFormat
}
