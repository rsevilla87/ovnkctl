package kube

import (
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type Client struct {
	Clientset  kubernetes.Interface
	RestConfig *rest.Config
}

func NewClient(flags *genericclioptions.ConfigFlags) (*Client, error) {
	config, err := flags.ToRESTConfig()
	if err != nil {
		return nil, err
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	return &Client{
		Clientset:  clientset,
		RestConfig: config,
	}, nil
}
