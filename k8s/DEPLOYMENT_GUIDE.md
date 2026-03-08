
# 完全重新部署指南

## 前置条件

- Kubernetes 集群（k3s 或其他）
- kubectl 已配置并能连接集群
- kubeseal 已安装（用于 Sealed Secrets）

## 部署步骤

### 1. 生成 Sealed Secrets（首次部署或修改密钥时）

```bash
# 安装 Sealed Secrets Controller（如果尚未安装）
kubectl apply -f https://github.com/bitnami-labs/sealed-secrets/releases/download/v0.27.1/controller.yaml

# 等待 controller 启动
kubectl wait --for=condition=ready pod -n kube-system -l name=sealed-secrets-controller --timeout=120s

# 运行加密脚本，生成所有 sealed-secrets.yaml
./scripts/seal-secrets.sh
```

### 2. 完全重新部署（清理并重新安装）

```bash
# 方式 A：完全删除后重新部署（推荐用于干净的环境）

# 1. 删除整个 namespace（会删除所有资源）
kubectl delete namespace cloud-media

# 2. 等待 namespace 完全删除
kubectl wait --for=delete namespace cloud-media --timeout=120s

# 3. 重新部署（使用根目录的 kustomization.yaml）
kubectl apply -k k8s/

# 4. 观察部署进度
kubectl get pods -n cloud-media -w
```

```bash
# 方式 B：逐步部署（适合调试）

# 1. 删除整个 namespace
kubectl delete namespace cloud-media

# 2. 等待删除完成
kubectl wait --for=delete namespace cloud-media --timeout=120s

# 3. 按顺序部署组件

# 步骤 1: 命名空间和基础组件
kubectl apply -f k8s/namespace.yaml
kubectl apply -k k8s/base/

# 等待基础组件就绪
kubectl wait --for=condition=ready pod -n cloud-media -l app=postgres --timeout=300s
kubectl wait --for=condition=ready pod -n cloud-media -l app=rabbitmq --timeout=300s
kubectl wait --for=condition=ready pod -n cloud-media -l app=minio --timeout=300s

# 步骤 2: 可观测性组件
kubectl apply -k k8s/observability/

# 等待可观测性组件就绪
kubectl wait --for=condition=ready pod -n cloud-media -l app=loki --timeout=300s
kubectl wait --for=condition=ready pod -n cloud-media -l app=tempo --timeout=300s
kubectl wait --for=condition=ready pod -n cloud-media -l app=mimir --timeout=300s
kubectl wait --for=condition=ready pod -n cloud-media -l app=grafana --timeout=300s
kubectl wait --for=condition=ready pod -n cloud-media -l app.kubernetes.io/name=opentelemetry-collector --timeout=300s

# 步骤 3: 应用服务
kubectl apply -k k8s/apps/

# 等待应用服务就绪
kubectl wait --for=condition=ready pod -n cloud-media -l app=api-server --timeout=300s
kubectl wait --for=condition=ready pod -n cloud-media -l app=worker --timeout=300s

# 步骤 4: Ingress
kubectl apply -k k8s/ingress/

# 步骤 5: KEDA（如果需要自动扩缩容）
# 先确保 KEDA controller 已安装
# kubectl apply -f https://github.com/kedacore/keda/releases/download/v2.11.2/keda-2.11.2.yaml
kubectl apply -k k8s/keda/
```

### 3. 验证部署

```bash
# 查看所有 Pod 状态
kubectl get pods -n cloud-media

# 查看所有 Service
kubectl get svc -n cloud-media

# 查看 Ingress
kubectl get ingress -n cloud-media

# 查看 KEDA ScaledObject
kubectl get scaledobject -n cloud-media

# 查看某个 Pod 的日志
kubectl logs -n cloud-media -l app=api-server
kubectl logs -n cloud-media -l app=worker
```

### 4. 更新配置或密钥后重新部署

```bash
# 1. 修改对应目录下的 secrets.unsealed.yaml
# 例如：vim k8s/apps/secrets.unsealed.yaml

# 2. 重新运行加密脚本
./scripts/seal-secrets.sh

# 3. 重新应用变更
kubectl apply -k k8s/

# 或者只更新特定部分
kubectl apply -k k8s/apps/
```

### 5. 常见问题排查

#### Pod 一直处于 Pending 状态

```bash
# 查看事件
kubectl describe pod -n cloud-media <pod-name>

# 检查 PVC 是否绑定
kubectl get pvc -n cloud-media
```

#### Pod 启动失败

```bash
# 查看 Pod 日志
kubectl logs -n cloud-media <pod-name>

# 查看详细描述
kubectl describe pod -n cloud-media <pod-name>
```

#### Secret 相关问题

```bash
# 确认 Secret 已创建
kubectl get secrets -n cloud-media

# 确认 SealedSecret 已创建
kubectl get sealedsecrets -n cloud-media

# 查看 Sealed Secrets controller 日志
kubectl logs -n kube-system -l name=sealed-secrets-controller
```

