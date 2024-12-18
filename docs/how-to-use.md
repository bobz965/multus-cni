#

## Multus CNI usage guide

### Prerequisites

* Kubelet configured to use CNI
* Kubernetes version with CRD support (generally )

Your Kubelet(s) must be configured to run with the CNI network plugin. Please see [Kubernetes document for CNI](https://kubernetes.io/docs/concepts/extend-kubernetes/compute-storage-net/network-plugins/#cni) for more details.

### Install Multus

Generally we recommend two options: Manually place a Multus binary in your `/opt/cni/bin`, or use our [quick-start method](quickstart.md) -- which creates a daemonset that has an opinionated way of how to install & configure Multus CNI (recommended).

#### Copy Multus Binary into place

You may acquire the Multus binary via compilation (see the [developer guide](development.md)) or download the a binary from the [GitHub releases](https://github.com/k8snetworkplumbingwg/multus-cni/releases) page. Copy multus binary into CNI binary directory, usually `/opt/cni/bin`. Perform this on all nodes in your cluster (master and nodes).

```bash
    cp multus /opt/cni/bin
```

#### Via Daemonset method

As a [quickstart](quickstart.md), you may apply these YAML files. Run this command (typically you would run this on the master, or wherever you have access to the `kubectl` command to manage your cluster).

```bash
    kubectl apply -f https://raw.githubusercontent.com/k8snetworkplumbingwg/multus-cni/master/deployments/multus-daemonset.yml  # thin deployment
```

or

```bash
    kubectl apply -f https://raw.githubusercontent.com/k8snetworkplumbingwg/multus-cni/master/deployments/multus-daemonset-thick.yml # thick (client/server) deployment
```

If you need more comprehensive detail, continue along with this guide, otherwise, you may wish to either [follow the quickstart guide](quickstart.md) or skip to the ['Create network attachment definition'](#create-network-attachment-definition) section.

### Set up conf file in /etc/cni/net.d/ (Installed automatically by Daemonset)

#### If you use daemonset to install multus, skip this section and go to "Create network attachment

You put CNI config file in `/etc/cni/net.d`. Kubernetes CNI runtime uses the alphabetically first file in the directory. (`"NOTE1"`, `"NOTE2"` are just comments, you can remove them at your configuration)

Execute following commands at all Kubernetes nodes (i.e. master and minions)

```bash
mkdir -p /etc/cni/net.d
cat >/etc/cni/net.d/00-multus.conf <<EOF
{
  "name": "multus-cni-network",
  "type": "multus",
  "readinessindicatorfile": "/run/flannel/subnet.env",
  "delegates": [
    {
      "NOTE1": "This is example, wrote your CNI config in delegates",
      "NOTE2": "If you use flannel, you also need to run flannel daemonset before!",
      "type": "flannel",
      "name": "flannel.1",
      "delegate": {
        "isDefaultGateway": true
      }
    }
  ],
  "kubeconfig": "/etc/cni/net.d/multus.d/multus.kubeconfig"
}
EOF
```

For the detail, please take a look into [Configuration Reference](configuration.md)

**NOTE: You can use "clusterNetwork"/"defaultNetworks" instead of "delegates"

As above config, you need to set `"kubeconfig"` in the config file for NetworkAttachmentDefinition(CRD).

##### Which network will be used for "Pod IP"?

In case of "delegates", the first delegates network will be used for "Pod IP". Otherwise, "clusterNetwork" will be used for "Pod IP".

#### Create ServiceAccount, ClusterRole and its binding

Create resources for multus to access CRD objects as following command:

```bash
# Execute following commands at Kubernetes master
cat <<EOF | kubectl create -f -
apiVersion: v1
kind: ServiceAccount
metadata:
  name: multus
  namespace: kube-system
---
kind: ClusterRole
apiVersion: rbac.authorization.k8s.io/v1beta1
metadata:
  name: multus
rules:
  - apiGroups: ["k8s.cni.cncf.io"]
    resources:
      - '*'
    verbs:
      - '*'
  - apiGroups:
      - ""
    resources:
      - pods
      - pods/status
    verbs:
      - get
      - update
---
kind: ClusterRoleBinding
apiVersion: rbac.authorization.k8s.io/v1beta1
metadata:
  name: multus
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: multus
subjects:
- kind: ServiceAccount
  name: multus
  namespace: kube-system
EOF
```

#### Set up kubeconfig file

Create kubeconfig at master node as following commands:

```bash
# Execute following command at Kubernetes master
mkdir -p /etc/cni/net.d/multus.d
SERVICEACCOUNT_CA=$(kubectl get secrets -n=kube-system -o json | jq -r '.items[]|select(.metadata.annotations."kubernetes.io/service-account.name"=="multus")| .data."ca.crt"')
SERVICEACCOUNT_TOKEN=$(kubectl get secrets -n=kube-system -o json | jq -r '.items[]|select(.metadata.annotations."kubernetes.io/service-account.name"=="multus")| .data.token' | base64 -d )
KUBERNETES_SERVICE_PROTOCOL=$(kubectl get all -o json | jq -r .items[0].spec.ports[0].name)
KUBERNETES_SERVICE_HOST=$(kubectl get all -o json | jq -r .items[0].spec.clusterIP)
KUBERNETES_SERVICE_PORT=$(kubectl get all -o json | jq -r .items[0].spec.ports[0].port)
cat > /etc/cni/net.d/multus.d/multus.kubeconfig <<EOF
# Kubeconfig file for Multus CNI plugin.
apiVersion: v1
kind: Config
clusters:
- name: local
  cluster:
    server: ${KUBERNETES_SERVICE_PROTOCOL:-https}://${KUBERNETES_SERVICE_HOST}:${KUBERNETES_SERVICE_PORT}
    certificate-authority-data: ${SERVICEACCOUNT_CA}
users:
- name: multus
  user:
    token: "${SERVICEACCOUNT_TOKEN}"
contexts:
- name: multus-context
  context:
    cluster: local
    user: multus
current-context: multus-context
EOF
```

Copy `/etc/cni/net.d/multus.d/multus.kubeconfig` into other Kubernetes nodes
**NOTE: Recommend to exec 'chmod 600 /etc/cni/net.d/multus.d/multus.kubeconfig' to keep secure**

```bash
scp /etc/cni/net.d/multus.d/multus.kubeconfig ...
```

### Setup CRDs (daemonset automatically does)

#### If you use daemonset to install multus, skip this section and go to "Create network attachment"**

Create CRD definition in Kubernetes as following command at master node:

```bash
# Execute following command at Kubernetes master
cat <<EOF | kubectl create -f -
apiVersion: apiextensions.k8s.io/v1beta1
kind: CustomResourceDefinition
metadata:
  name: network-attachment-definitions.k8s.cni.cncf.io
spec:
  group: k8s.cni.cncf.io
  version: v1
  scope: Namespaced
  names:
    plural: network-attachment-definitions
    singular: network-attachment-definition
    kind: NetworkAttachmentDefinition
    shortNames:
    - net-attach-def
  validation:
    openAPIV3Schema:
      properties:
        spec:
          properties:
            config:
                 type: string
EOF
```

### Create network attachment definition

The 'NetworkAttachmentDefinition' is used to setup the network attachment, i.e. secondary interface for the pod, There are two ways to configure the 'NetworkAttachmentDefinition' as following:

* NetworkAttachmentDefinition with json CNI config
* NetworkAttachmentDefinition with CNI config file

#### NetworkAttachmentDefinition with json CNI config

Following command creates NetworkAttachmentDefinition. CNI config is in `config:` field.

```bash
# Execute following command at Kubernetes master
cat <<EOF | kubectl create -f -
apiVersion: "k8s.cni.cncf.io/v1"
kind: NetworkAttachmentDefinition
metadata:
  name: macvlan-conf-1
spec:
  config: '{
            "cniVersion": "0.3.0",
            "type": "macvlan",
            "master": "eth1",
            "mode": "bridge",
            "ipam": {
                "type": "host-local",
                "ranges": [
                    [ {
                         "subnet": "10.10.0.0/16",
                         "rangeStart": "10.10.1.20",
                         "rangeEnd": "10.10.3.50",
                         "gateway": "10.10.0.254"
                    } ]
                ]
            }
        }'
EOF
```

#### NetworkAttachmentDefinition with CNI config file

If NetworkAttachmentDefinition has no spec, multus find a file in defaultConfDir ('/etc/cni/multus/net.d', with same name in the 'name' field of CNI config.

```bash
# Execute following command at Kubernetes master
cat <<EOF | kubectl create -f -
apiVersion: "k8s.cni.cncf.io/v1"
kind: NetworkAttachmentDefinition
metadata:
  name: macvlan-conf-2
EOF
```

```bash
# Execute following commands at all Kubernetes nodes (i.e. master and minions)
cat <<EOF > /etc/cni/multus/net.d/macvlan2.conf
{
  "cniVersion": "0.3.0",
  "type": "macvlan",
  "name": "macvlan-conf-2",
  "master": "eth1",
  "mode": "bridge",
  "ipam": {
      "type": "host-local",
      "ranges": [
          [ {
               "subnet": "11.10.0.0/16",
               "rangeStart": "11.10.1.20",
               "rangeEnd": "11.10.3.50"
          } ]
      ]
  }
}
EOF
```

### Run pod with network annotation

#### Launch pod with text annotation

```bash
# Execute following command at Kubernetes master
cat <<EOF | kubectl create -f -
apiVersion: v1
kind: Pod
metadata:
  name: pod-case-01
  annotations:
    k8s.v1.cni.cncf.io/networks: macvlan-conf-1, macvlan-conf-2
spec:
  containers:
  - name: pod-case-01
    image: docker.io/centos/tools:latest
    command:
    - /sbin/init
EOF
```

#### Launch pod with text annotation for NetworkAttachmentDefinition in different namespace

You can also specify NetworkAttachmentDefinition with its namespace as adding `<namespace>/`

```bash
# Execute following command at Kubernetes master
cat <<EOF | kubectl create -f -
apiVersion: "k8s.cni.cncf.io/v1"
kind: NetworkAttachmentDefinition
metadata:
  name: macvlan-conf-3
  namespace: testns1
spec:
  config: '{
            "cniVersion": "0.3.0",
            "type": "macvlan",
            "master": "eth1",
            "mode": "bridge",
            "ipam": {
                "type": "host-local",
                "ranges": [
                    [ {
                         "subnet": "12.10.0.0/16",
                         "rangeStart": "12.10.1.20",
                         "rangeEnd": "12.10.3.50"
                    } ]
                ]
            }
        }'
EOF
cat <<EOF | kubectl create -f -
apiVersion: v1
kind: Pod
metadata:
  name: pod-case-02
  annotations:
    k8s.v1.cni.cncf.io/networks: testns1/macvlan-conf-3
spec:
  containers:
  - name: pod-case-02
    image: docker.io/centos/tools:latest
    command:
    - /sbin/init
EOF
```

#### Launch pod with text annotation with interface name

You can also specify interface name as adding `@<ifname>`.

```bash
# Execute following command at Kubernetes master
cat <<EOF | kubectl create -f -
apiVersion: v1
kind: Pod
metadata:
  name: pod-case-03
  annotations:
    k8s.v1.cni.cncf.io/networks: macvlan-conf-1@macvlan1
spec:
  containers:
  - name: pod-case-03
    image: docker.io/centos/tools:latest
    command:
    - /sbin/init
EOF
```

#### Launch pod with json annotation

```bash
# Execute following command at Kubernetes master
cat <<EOF | kubectl create -f -
apiVersion: v1
kind: Pod
metadata:
  name: pod-case-04
  annotations:
    k8s.v1.cni.cncf.io/networks: '[
            { "name" : "macvlan-conf-1" },
            { "name" : "macvlan-conf-2" }
    ]'
spec:
  containers:
  - name: pod-case-04
    image: docker.io/centos/tools:latest
    command:
    - /sbin/init
EOF
```

#### Launch pod with json annotation for NetworkAttachmentDefinition in different namespace

You can also specify NetworkAttachmentDefinition with its namespace as adding `"namespace": "<namespace>"`.

```bash
# Execute following command at Kubernetes master
cat <<EOF | kubectl create -f -
apiVersion: v1
kind: Pod
metadata:
  name: pod-case-05
  annotations:
    k8s.v1.cni.cncf.io/networks: '[
            { "name" : "macvlan-conf-1",
              "namespace": "testns1" }
    ]'
spec:
  containers:
  - name: pod-case-05
    image: docker.io/centos/tools:latest
    command:
    - /sbin/init
EOF
```

#### Launch pod with json annotation with interface

You can also specify interface name as adding `"interface": "<ifname>"`.

```bash
# Execute following command at Kubernetes master
cat <<EOF | kubectl create -f -
apiVersion: v1
kind: Pod
metadata:
  name: pod-case-06
  annotations:
    k8s.v1.cni.cncf.io/networks: '[
            { "name" : "macvlan-conf-1",
              "interface": "macvlan1" },
            { "name" : "macvlan-conf-2" }
    ]'
spec:
  containers:
  - name: pod-case-06
    image: docker.io/centos/tools:latest
    command:
    - /sbin/init
EOF
```

### Verifying pod network

Following the example of `ip -d address` output of above pod, "pod-case-06":

```bash
# Execute following command at Kubernetes master
kubectl exec -it pod-case-06 -- ip -d address

1: lo: <LOOPBACK,UP,LOWER_UP> mtu 65536 qdisc noqueue state UNKNOWN group default qlen 1
    link/loopback 00:00:00:00:00:00 brd 00:00:00:00:00:00 promiscuity 0 numtxqueues 1 numrxqueues 1 gso_max_size 65536 gso_max_segs 65535
    inet 127.0.0.1/8 scope host lo
       valid_lft forever preferred_lft forever
    inet6 ::1/128 scope host
       valid_lft forever preferred_lft forever
3: eth0@if11: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1450 qdisc noqueue state UP group default
    link/ether 0a:58:0a:f4:02:06 brd ff:ff:ff:ff:ff:ff link-netnsid 0 promiscuity 0
    veth numtxqueues 1 numrxqueues 1 gso_max_size 65536 gso_max_segs 65535
    inet 10.244.2.6/24 scope global eth0
       valid_lft forever preferred_lft forever
    inet6 fe80::ac66:45ff:fe7c:3a19/64 scope link
       valid_lft forever preferred_lft forever
4: macvlan1@if3: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500 qdisc noqueue state UNKNOWN group default
    link/ether 4e:6d:7a:4e:14:87 brd ff:ff:ff:ff:ff:ff link-netnsid 0 promiscuity 0
    macvlan mode bridge numtxqueues 1 numrxqueues 1 gso_max_size 65536 gso_max_segs 65535
    inet 10.10.1.22/16 scope global macvlan1
       valid_lft forever preferred_lft forever
    inet6 fe80::4c6d:7aff:fe4e:1487/64 scope link
       valid_lft forever preferred_lft forever
5: net2@if3: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500 qdisc noqueue state UNKNOWN group default
    link/ether 6e:e3:71:7f:86:f7 brd ff:ff:ff:ff:ff:ff link-netnsid 0 promiscuity 0
    macvlan mode bridge numtxqueues 1 numrxqueues 1 gso_max_size 65536 gso_max_segs 65535
    inet 11.10.1.22/16 scope global net2
       valid_lft forever preferred_lft forever
    inet6 fe80::6ce3:71ff:fe7f:86f7/64 scope link
       valid_lft forever preferred_lft forever
```

| Interface name | Description |
| --- | --- |
| lo | loopback |
| eth0 | Default network interface (flannel) |
| macvlan1 | macvlan interface (macvlan-conf-1) |
| net2 | macvlan interface (macvlan-conf-2) |

## Specifying a default route for a specific attachment

Typically, the default route for a pod will route traffic over the `eth0` and therefore over the cluster-wide default network. You may wish to specify that a different network attachment will have the default route.

You can achieve this by using the JSON formatted annotation and specifying a `default-route` key.

*NOTE*: It's important that you consider that this may impact some functionality of getting traffic to route over the cluster-wide default network.

For example, we have a this configuration for macvlan:

```bash
cat <<EOF | kubectl create -f -
apiVersion: "k8s.cni.cncf.io/v1"
kind: NetworkAttachmentDefinition
metadata:
  name: macvlan-conf
spec:
  config: '{
      "cniVersion": "0.3.0",
      "type": "macvlan",
      "master": "eth0",
      "mode": "bridge",
      "ipam": {
        "type": "host-local",
        "subnet": "192.168.2.0/24",
        "rangeStart": "192.168.2.200",
        "rangeEnd": "192.168.2.216",
        "routes": [
          { "dst": "0.0.0.0/0" }
        ],
        "gateway": "192.168.2.1"
      }
    }'
EOF
```

We can then create a pod which uses the `default-route` key in the JSON formatted `k8s.v1.cni.cncf.io/networks` annotation.

```bash
cat <<EOF | kubectl create -f -
apiVersion: v1
kind: Pod
metadata:
  name: samplepod
  annotations:
    k8s.v1.cni.cncf.io/networks: '[{
      "name": "macvlan-conf",
      "default-route": ["192.168.2.1"]
    }]'
spec:
  containers:
  - name: samplepod
    command: ["/bin/bash", "-c", "trap : TERM INT; sleep infinity & wait"]
    image: dougbtv/centos-network
EOF
```

This will set `192.168.2.1` as the default route over the `net1` interface, such as:

```bash
kubectl exec -it samplepod -- ip route

default via 192.168.2.1 dev net1
10.244.0.0/24 dev eth0  proto kernel  scope link  src 10.244.0.169
10.244.0.0/16 via 10.244.0.1 dev eth0
```

## Entrypoint Parameters

Multus CNI, when installed using the daemonset-style installation uses an entrypoint script which copies the Multus binary into place, places CNI configurations. This entrypoint takes a variety of parameters for customization.

Typically, you'd modified the daemonset YAML itself to specify these parameters.

For example, the `command` and `args` parameters in the `containers` section of the DaemonSet may look something like:

```bash
  command: ["/thin_entrypoint"]
  args:
  - "--multus-conf-file=auto"
  - "--namespace-isolation=true"
  - "--multus-log-level=verbose"
```

Note that some of the defaults have directories inside the root directory named `/host/`, this is because it is deployed as a container and we have host file system locations mapped into this directory inside the container. If you use other directories, you may have to change the mounted volumes.

### Entrypoint script parameters

Each parameter is shown with the default as the value.

```bash
    --cni-conf-dir=/host/etc/cni/net.d
```

This is the configuration directory where Multus will write its configuration file.

```bash
    --cni-bin-dir=/host/opt/cni/bin
```

This the directory in which the Multus binary will be installed.

```bash
    --namespace-isolation=false
```

Setting this option to true enables the Namespace isolation feature, which insists that custom resources must be created in the same namespace as the pods, otherwise it will refuse to attach those definitions as additional interfaces. See [the configuration guide for more information](configuration.md).

```bash
    --global-namespaces=default,foo,bar
```

The `--global-namespaces` works only when `--namespace-isolation=true`. This takes a comma-separated list of namespaces which can be referred to globally when namespace isolation is enabled. See [the configuration guide for more information](configuration.md).

```bash
    --multus-bin-file=/usr/src/multus-cni/bin/multus
```

This option lets you set which binary executable to copy from the container onto the host (into the directory specified by `--cni-bin-dir`), allowing one to copy an alternate version or build of Multus CNI.

```bash
    --multus-conf-file=/usr/src/multus-cni/images/70-multus.conf
```

The `--multus-conf-file` is one of two options; it can be set to a source file to be copied into the location specified by `--cni-conf-dir`. Or, to a value of `auto`, that is: `--multus-conf-file=auto`.

The automatic configuration option is used to automatically generate Multus configurations given existing on-disk CNI configurations for your default network.

In the case that `--multus-conf-file=auto` -- The entrypoint script will look at the `--multus-autoconfig-dir` (by default, the same as the `--cni-conf-dir`). Multus will take the alphabetically first configuration there and wrap that into a Multus configuration.

```bash
    --multus-autoconfig-dir=/host/etc/cni/net.d
```

Used only with `--multus-conf-file=auto`. This option allows one to set which directory will be used to generate configuration files.

This can be used if you have your CNI configuration stored in an alternate location, or, you have constraints on race conditions where you'd like to generate your default network configuration first, and then only have Multus write its configuration when it finds that configuration -- allowing only Multus to write the CNI configuration in the `--cni-conf-dir`, therefore notifying the Kubelet that the node is in a ready state.

```bash
    --multus-kubeconfig-file-host=/etc/cni/net.d/multus.d/multus.kubeconfig
```

Used only with `--multus-conf-file=auto`. Allows you to specify an alternate path to the Kubeconfig.

```bash
    --multus-master-cni-file-name=
```

The `--multus-master-cni-file-name` can be used to select the cni file as the master cni, rather than the first file in cni-conf-dir. For example, `--multus-master-cni-file-name=10-calico.conflist`.

```bash
    --multus-log-level=
    --multus-log-file=
```

Used only with `--multus-conf-file=auto`. See the [documentation for logging](https://github.com/k8snetworkplumbingwg/multus-cni/blob/master/docs/configuration.md#logging) for which values are permitted.

Used only with `--multus-conf-file=auto`. Allows you to specify CNI spec version. Please set if you need to specify CNI spec version.

```bash
    --cni-version=
```

In some cases, the original CNI configuration that the Multus configuration was generated from (using `--multus-conf-file=auto`) may be used as a sort of semaphor for network readiness -- as this model is used by the Kubelet itself. If you need to disable Multus' availability, you may wish to clean out the generated configuration file when the source file for autogeneration of the config file is no longer present. You can use this functionality by setting:

```bash
    --cleanup-config-on-exit=true
```

When specifying `--cleanup-config-on-exit=true` the entrypoint script will delete any generated/copied Multus configuration files when entrypoint script
exits (upon Pod termination). This allows Multus to be safely removed from the cluster when its no longer needed.

In addition, when both `--cleanup-config-on-exit=true` and `--multus-conf-file=auto` are specified, the entrypoint script will watch for changes of the
master CNI configuration and kubeconfig. when such change detected, the script will re-genereate Multus configuration. Watch can be skipped by setting:

```bash
    --skip-config-watch
```

Additionally when using CRIO, you may wish to have the CNI config file that's used as the source for `--multus-conf-file=auto` renamed. This boolean option when set to true automatically renames the file with a `.old` suffix to the original filename.

```bash
    --rename-conf-file=true
```

When using `--multus-conf-file=auto` you may also care to specify a `binDir` in the configuration, this can be accomplished using the `--additional-bin-dir` option.

```bash
    --additional-bin-dir=/opt/multus/bin
```

Sometimes, you may wish to not have the entrypoint copy the binary file onto the host. Potentially, you have another way to copy in a specific version of Multus, for example. By default, it's always copied, but you may disable the copy with:

```bash
    --skip-multus-binary-copy=true
```

If you wish to have auto configuration use the `readinessindicatorfile` in the configuration, you can use the `--readiness-indicator-file` to express which file should be used as the readiness indicator.

```bash
    --readiness-indicator-file=/path/to/file
```

### Run pod with network annotation and Dynamic Resource Allocation driver

> :warning: Dynamic Resource Allocation (DRA) is [currently an alpha](https://kubernetes.io/docs/concepts/scheduling-eviction/dynamic-resource-allocation/),
> and is subject to change. Please consider this functionality as a preview. The architecture and usage of DRA in
> Multus CNI may change in the future as this technology matures.
>
> The current DRA integration is based on the DRA API for Kubernetes 1.26 to 1.30. With Kubernetes 1.31, the DRA API
> will change and multus doesn't integrate with the new API yet.

Dynamic Resource Allocation is alternative mechanism to device plugin which allows to requests pod and container
resources.

The following sections describe how to use DRA with multus and NVIDIA DRA driver. Other DRA networking driver vendors
should follow similar concepts to make use of multus DRA support.

#### Prerequisite

1. Kubernetes 1.27
2. Container Runtime with CDI support enabled
3. Kubernetes runtime-config=resource.k8s.io/v1alpha2
4. Kubernetes feature-gates=DynamicResourceAllocation=True,KubeletPodResourcesDynamicResources=true

#### Install DRA driver

The current example uses NVIDIA DRA driver for networking. This DRA driver is not publicly available. An alternative to
this DRA driver is available at [dra-example-driver](https://github.com/kubernetes-sigs/dra-example-driver).

#### Create dynamic resource class with NVIDIA network DRA driver

The `ResourceClass` defines the resource pool of `sf-pool-1`.

```bash
# Execute following command at Kubernetes master
cat <<EOF | kubectl create -f -
apiVersion: resource.k8s.io/v1alpha2
kind: ResourceClass
metadata:
  name: sf-pool-1
driverName: net.resource.nvidia.com
EOF
```

#### Create network attachment definition with resource name

The `k8s.v1.cni.cncf.io/resourceName` should match the `ResourceClass` name defined in the section above.
In this example it is `sf-pool-1`. Multus query the K8s PodResource API to fetch the `resourceClass` name and also
query the NetworkAttachmentDefinition `k8s.v1.cni.cncf.io/resourceName`. If both has the same name multus send the
CDI device name in the DeviceID argument.

##### NetworkAttachmentDefinition for ovn-kubernetes example

Following command creates NetworkAttachmentDefinition. CNI config is in `config:` field.

```bash
# Execute following command at Kubernetes master
cat <<EOF | kubectl create -f -
apiVersion: "k8s.cni.cncf.io/v1"
kind: NetworkAttachmentDefinition
metadata:
  name: default
  annotations:
    k8s.v1.cni.cncf.io/resourceName: sf-pool-1
spec:
  config: '{
      "cniVersion": "0.4.0",
      "dns": {},
      "ipam": {},
      "logFile": "/var/log/ovn-kubernetes/ovn-k8s-cni-overlay.log",
      "logLevel": "4",
      "logfile-maxage": 5,
      "logfile-maxbackups": 5,
      "logfile-maxsize": 100,
      "name": "ovn-kubernetes",
      "type": "ovn-k8s-cni-overlay"
    }'
EOF
```

#### Create DRA Resource Claim

Following command creates `ResourceClaim` `sf` which request resource from  `ResourceClass` `sf-pool-1`.

```bash
# Execute following command at Kubernetes master
cat <<EOF | kubectl create -f -
apiVersion: resource.k8s.io/v1alpha2
kind: ResourceClaim
metadata:
  namespace: default
  name: sf
spec:
  spec:
    resourceClassName: sf-pool-1
EOF
```

#### Launch pod with DRA Resource Claim

Following command Launch a Pod with primiry network `default` and `ResourceClaim` `sf`.

```yaml
apiVersion: v1
kind: Pod
metadata:
  namespace: default
  name: test-sf-claim
  annotations:
    v1.multus-cni.io/default-network: default
spec:
  restartPolicy: Always
  containers:
  - name: with-resource
    image: docker.io/library/ubuntu:22.04
    command: ["/bin/sh", "-ec", "while :; do echo '.'; sleep 5 ; done"]
    resources:
      claims:
      - name: resource
  resourceClaims:
  - name: resource
    source:
      resourceClaimName: sf
```
