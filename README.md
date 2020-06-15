# Kubernetes Webhook to configure IBM COS Firewall 

## Prerequisites
1. Local system should be Linux or Mac
2. Git and GoLang `v1.13.5` should be installed
3. `kubectl` or `oc` CLI should be installed on your local system

## Build the Image from Sources (optional)
1. Export the  DockerHub your registry name,
   ```
   $ export DOCKER_REG="<Your DockerHub Registry>"
   ```

2. Build and push the image
   ```
   $ make push-image
   ```

## Deploying the Webhook Server
1. Log into OpenShift Cluster using `oc` command or export the `KUBECONFIG`

2. Export the  DockerHub registry name (you can export your registry if you build your own image) 
   ```
   $ export DOCKER_REG="nkkashyap"
   ```

2. Deploy the webhook
   ```
   ./deploy.sh
   ```

## Verify

1. The `webhook-server` pod in the `webhook-admin` namespace should be running:
```
$ oc -n webhook-admin get pods
NAME                             READY     STATUS    RESTARTS   AGE
webhook-server-767f99b798-j2f4   1/1       Running   0          35m
```

2. A `MutatingWebhookConfiguration` named `demo-webhook` should exist:
```
$ oc get mutatingwebhookconfigurations
NAME           AGE
demo-webhook   36m
```

3. Create COS Secret
```
apiVersion: v1
kind: Secret
metadata:
  name: cos-cred-rw
type: ibm/ibmc-s3fs
data:
  access-key: <base64 encoded HMAC access_key_id>
  secret-key: <base64 encoded HMAC secret_access_key>
  res-conf-apikey: <base64 encoded apikey with Manager Role>
stringData:
  allowed_ips: "10.177.213.184,10.73.237.220,10.74.22.72" # List of Worker Node IPs
```

4. Create PVC
```
kind: PersistentVolumeClaim
apiVersion: v1
metadata:
  name: pvc-mybucket-test01
  annotations:
    ibm.io/auto-create-bucket: "true"
    ibm.io/auto-delete-bucket: "false"
    ibm.io/bucket: "mybucket-test01"
    ibm.io/region: "us-standard"
    ibm.io/secret-name: "cos-cred-rw"
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 10Gi
  storageClassName: ibmc-s3fs-standard-perf-regional
  volumeMode: Filesystem
```
