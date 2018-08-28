package opsservice

import (
	"github.com/gravitational/gravity/lib/constants"
	"github.com/gravitational/gravity/lib/defaults"
	"github.com/gravitational/gravity/lib/ops"
	"github.com/gravitational/rigging"
	"github.com/gravitational/trace"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// DeleteClusterCertificate deletes cluster certificate
func (o *Operator) DeleteClusterCertificate(key ops.SiteKey) error {
	client, err := o.GetKubeClient()
	if err != nil {
		return trace.Wrap(err)
	}
	return DeleteClusterCertificate(client)
}

// GetClusterCertificate returns the cluster certificate
func (o *Operator) GetClusterCertificate(key ops.SiteKey, withSecrets bool) (*ops.ClusterCertificate, error) {
	client, err := o.GetKubeClient()
	if err != nil {
		return nil, trace.Wrap(err)
	}

	certificate, privateKey, err := GetClusterCertificate(client)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	if !withSecrets {
		privateKey = nil
	}

	return &ops.ClusterCertificate{
		Certificate: certificate,
		PrivateKey:  privateKey,
	}, nil
}

// UpdateClusterCertificate updates the cluster certificate
func (o *Operator) UpdateClusterCertificate(req ops.UpdateCertificateRequest) (*ops.ClusterCertificate, error) {
	client, err := o.GetKubeClient()
	if err != nil {
		return nil, trace.Wrap(err)
	}

	err = UpdateClusterCertificate(client, req)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	return &ops.ClusterCertificate{
		Certificate: req.Certificate,
	}, nil
}

// GetClusterCertificate returns certificate and private key data stored in a secret
// inside the cluster
//
// The method is supposed to be called from within deployed Kubernetes cluster
func GetClusterCertificate(client *kubernetes.Clientset) ([]byte, []byte, error) {
	secret, err := client.Core().Secrets(defaults.KubeSystemNamespace).Get(constants.ClusterCertificateMap, metav1.GetOptions{})
	if err != nil {
		return nil, nil, trace.Wrap(rigging.ConvertError(err))
	}

	certificateData, ok := secret.Data[constants.ClusterCertificateMapKey]
	if !ok {
		return nil, nil, trace.NotFound("cluster certificate not found")
	}

	privateKeyData, ok := secret.Data[constants.ClusterPrivateKeyMapKey]
	if !ok {
		return nil, nil, trace.NotFound("cluster private key not found")
	}

	return certificateData, privateKeyData, nil
}

//
// DeleteClusterCertificate deletes cluster certificate
//
func DeleteClusterCertificate(client *kubernetes.Clientset) error {
	err := client.Core().Secrets(defaults.KubeSystemNamespace).Delete(constants.ClusterCertificateMap, nil)
	if err != nil {
		return trace.Wrap(rigging.ConvertError(err))
	}
	return nil
}

// UpdateClusterCertificate updates the cluster certificate and private key
//
// The method is supposed to be called from within deployed Kubernetes cluster
func UpdateClusterCertificate(client *kubernetes.Clientset, req ops.UpdateCertificateRequest) error {
	err := req.Check()
	if err != nil {
		return trace.Wrap(err)
	}

	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      constants.ClusterCertificateMap,
			Namespace: defaults.KubeSystemNamespace,
		},
		Data: map[string][]byte{
			constants.ClusterCertificateMapKey: append(req.Certificate, req.Intermediate...),
			constants.ClusterPrivateKeyMapKey:  req.PrivateKey,
		},
		Type: v1.SecretTypeOpaque,
	}

	_, err = client.Core().Secrets(defaults.KubeSystemNamespace).Create(secret)
	if err != nil {
		if !trace.IsAlreadyExists(rigging.ConvertError(err)) {
			return trace.Wrap(err)
		}
		_, err = client.Core().Secrets(defaults.KubeSystemNamespace).Update(secret)
		if err != nil {
			return trace.Wrap(rigging.ConvertError(err))
		}
	}

	return nil
}
