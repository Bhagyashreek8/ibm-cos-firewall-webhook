/*
Copyright (c) 2019,2020 StackRox Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

   http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	// "context"
	"errors"
	"fmt"
	"k8s.io/api/admission/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"log"
	"net/http"
	"path/filepath"
	"strings"
)

const (
	tlsDir      = `/run/secrets/tls`
	tlsCertFile = `tls.crt`
	tlsKeyFile  = `tls.key`
)

var (
	goClient *kubernetes.Clientset
	podResource = metav1.GroupVersionResource{Version: "v1", Resource: "pods"}
	pvResource = metav1.GroupVersionResource{Version: "v1", Resource: "persistentvolumes"}
)

// applyPODSecurity implements the logic of our example admission controller webhook. For every pod that is created
// (outside of Kubernetes namespaces), it first checks if `runAsNonRoot` is set. If it is not, it is set to a default
// value of `false`. Furthermore, if `runAsUser` is not set (and `runAsNonRoot` was not initially set), it defaults
// `runAsUser` to a value of 1234.
//
// To demonstrate how requests can be rejected, this webhook further validates that the `runAsNonRoot` setting does
// not conflict with the `runAsUser` setting - i.e., if the former is set to `true`, the latter must not be `0`.
// Note that we combine both the setting of defaults and the check for potential conflicts in one webhook; ideally,
// the latter would be performed in a validating webhook admission controller.
func applyPODSecurity(req *v1beta1.AdmissionRequest) ([]patchOperation, error) {
	log.Printf("--applyPODSecurity--")
	// This handler should only get called on Pod objects as per the MutatingWebhookConfiguration in the YAML file.
	// However, if (for whatever reason) this gets invoked on an object of a different kind, issue a log message but
	// let the object request pass through otherwise.
	if req.Resource != podResource {
		log.Printf("expect resource to be %s", podResource)
		return nil, nil
	}

	// Parse the Pod object.
	raw := req.Object.Raw
	pod := corev1.Pod{}
	if _, _, err := universalDeserializer.Decode(raw, nil, &pod); err != nil {
		return nil, fmt.Errorf("could not deserialize pod object: %v", err)
	}

	// Retrieve the `runAsNonRoot` and `runAsUser` values.
	var runAsNonRoot *bool
	var runAsUser *int64
	if pod.Spec.SecurityContext != nil {
		runAsNonRoot = pod.Spec.SecurityContext.RunAsNonRoot
		runAsUser = pod.Spec.SecurityContext.RunAsUser
	}

	// Create patch operations to apply sensible defaults, if those options are not set explicitly.
	var patches []patchOperation
	if runAsNonRoot == nil {
		patches = append(patches, patchOperation{
			Op:    "add",
			Path:  "/spec/securityContext/runAsNonRoot",
			// The value must not be true if runAsUser is set to 0, as otherwise we would create a conflicting
			// configuration ourselves.
			Value: runAsUser == nil || *runAsUser != 0,
		})

		if runAsUser == nil {
			patches = append(patches, patchOperation{
				Op:    "add",
				Path:  "/spec/securityContext/runAsUser",
				Value: 1234,
			})
		}
	} else if *runAsNonRoot == true && (runAsUser != nil && *runAsUser == 0) {
		// Make sure that the settings are not contradictory, and fail the object creation if they are.
		return nil, errors.New("runAsNonRoot specified, but runAsUser set to 0 (the root user)")
	}

	return patches, nil
}

func applyPVSecurity(req *v1beta1.AdmissionRequest) ([]patchOperation, error) {
	log.Printf("--applyPVSecurity--")

	var confFW bool = false
	var bucketName, secretName, secretNameSpace string
	var resConfApiKey, allowedIPs string

	if req.Resource != pvResource {
		log.Printf("expect resource to be %s", pvResource)
		return nil, nil
	}

	// Parse the Pod object.
	raw := req.Object.Raw
	pv := corev1.PersistentVolume{}
	if _, _, err := universalDeserializer.Decode(raw, nil, &pv); err != nil {
		return nil, fmt.Errorf("could not deserialize pv object: %v", err)
	}
	// https://godoc.org/k8s.io/api/core/v1#PersistentVolume
	// https://godoc.org/k8s.io/apimachinery/pkg/apis/meta/v1#ObjectMeta
	// https://godoc.org/k8s.io/api/core/v1#PersistentVolumeSpec
	// https://godoc.org/k8s.io/api/core/v1#PersistentVolumeSource
	log.Printf("Info: PV Name %s", pv.Name)
	if pv.Spec.FlexVolume != nil {
		if strings.Contains(pv.Spec.FlexVolume.Driver, "ibmc-s3fs") {
			confFW = true
			log.Printf("Info: IBM Cloud S3FS Driver %s", pv.Spec.FlexVolume.Driver)
			if key, ok := pv.Spec.FlexVolume.Options["bucket"]; ok {
				bucketName = key
			}
			if pv.Spec.FlexVolume.SecretRef != nil {
				secretName = pv.Spec.FlexVolume.SecretRef.Name
				secretNameSpace = pv.Spec.FlexVolume.SecretRef.Namespace
			} else {
				confFW = false
				log.Printf("Warn: Secret not set for %s", pv.Name)
			}
		} else {
			log.Printf("Info: Other Driver %s", pv.Spec.FlexVolume.Driver)
		}
		if confFW {
			log.Printf("Info: PV Bucket Name %s", bucketName)
			log.Printf("Info: PV Secret Name %s", pv.Spec.FlexVolume.SecretRef.Name)
			log.Printf("Info: PV Secret NameSpace %s", pv.Spec.FlexVolume.SecretRef.Namespace)
		}
	} else {
		log.Printf("Info: Not a FlexVolume %s", pv.Name)
	}
	if !confFW {
		return nil, nil
	}
	if len(secretName) > 0  && len(secretNameSpace) > 0 {
		// pvSecret, err := goClient.CoreV1().Secrets(secretNameSpace).Get(context.TODO(), secretName, metav1.GetOptions{})
		pvSecret, err := goClient.CoreV1().Secrets(secretNameSpace).Get(secretName, metav1.GetOptions{})
		if err == nil {
			if key, ok := pvSecret.Data["res-conf-apikey"]; ok {
				resConfApiKey = string(key)
			} else {
				log.Printf("Warn: res-conf-apikey not set for %s", pv.Name)
			}
			if ips, ok := pvSecret.Data["allowed_ips"]; ok	{
				allowedIPs   = string(ips)
			} else {
				log.Printf("Warn: allowed_ips not set for %s", pv.Name)
			}
		} else {
			log.Printf("Error: Cannot retrieve Whitelist IPs for %s", pv.Name)
		}
	}

	if len(allowedIPs) > 0 && len(resConfApiKey) > 0 {
		err := UpdateFirewallRules(allowedIPs, resConfApiKey, bucketName)
		if err != nil {
			fmt.Println("Error:", err)
			log.Printf("Error: Cannot configure firewall for %s", pv.Name)
		}
	}
	return nil, nil
}

func main() {
	certPath := filepath.Join(tlsDir, tlsCertFile)
	keyPath := filepath.Join(tlsDir, tlsKeyFile)
	config, err := rest.InClusterConfig()
	if err != nil {
		log.Fatal("Error: Cannot initialize server")
	}
	goClient, err = kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatal("Error: Cannot initialize server")
	}
	mux := http.NewServeMux()
	// mux.Handle("/podmutate", admitFuncHandler(applyPODSecurity))
	mux.Handle("/pvmutate", admitFuncHandler(applyPVSecurity))
	log.Printf("--Started WebHook Server--")
	server := &http.Server{
		// We listen on port 8443 such that we do not need root privileges or extra capabilities for this server.
		// The Service object will take care of mapping this port to the HTTPS port 443.
		Addr:    ":8443",
		Handler: mux,
	}
	log.Fatal(server.ListenAndServeTLS(certPath, keyPath))
}
