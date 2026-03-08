
# 各服务密钥需求清单

## 服务密钥总览

| 目录 | Secret 名称 | 用途 |
|------|-----------|------|
| `base/postgres/` | `postgres-secret` | PostgreSQL 数据库凭证 |
| `base/rabbitmq/` | `rabbitmq-secret` | RabbitMQ 消息队列凭证 |
| `base/minio/` | `minio-secret` | MinIO 对象存储根凭证 |
| `apps/` | `cloud-media-secret` | 应用服务（API Server + Worker）凭证 |
| `keda/` | `keda-rabbitmq-secret` | KEDA RabbitMQ 触发器连接 |
| `observability/` | `minio-credentials` | Loki/Tempo/Mimir S3 凭证 |
| `observability/` | `grafana-credentials` | Grafana 管理员凭证 |

---

## 各服务详细密钥需求

### 1. PostgreSQL (base/postgres/)

**Secret 名称**: `postgres-secret`

| 环境变量 / 密钥 | 说明 | StatefulSet |
|----------------|------|------------|
| `POSTGRES_USER` | 数据库用户名 | ✅ postgres/statefulset.yaml |
| `POSTGRES_PASSWORD` | 数据库密码 | ✅ postgres/statefulset.yaml |
| `POSTGRES_DB` | 数据库名 | ✅ postgres/statefulset.yaml |

---

### 2. RabbitMQ (base/rabbitmq/)

**Secret 名称**: `rabbitmq-secret`

| 环境变量 / 密钥 | 说明 | StatefulSet |
|----------------|------|------------|
| `RABBITMQ_DEFAULT_USER` | 默认用户名 | ✅ rabbitmq/statefulset.yaml |
| `RABBITMQ_DEFAULT_PASS` | 默认密码 | ✅ rabbitmq/statefulset.yaml |

---

### 3. MinIO (base/minio/)

**Secret 名称**: `minio-secret`

| 环境变量 / 密钥 | 说明 | StatefulSet |
|----------------|------|------------|
| `MINIO_ROOT_USER` | 根用户名 | ✅ minio/statefulset.yaml |
| `MINIO_ROOT_PASSWORD` | 根密码 | ✅ minio/statefulset.yaml |

---

### 4. 应用服务 (apps/)

**Secret 名称**: `cloud-media-secret`

| 环境变量 / 密钥 | 说明 | 使用方 |
|----------------|------|--------|
| `CLOUD_MEDIA_DATABASE_USER` | 数据库用户名 | api-server, worker |
| `CLOUD_MEDIA_DATABASE_PASSWORD` | 数据库密码 | api-server, worker |
| `CLOUD_MEDIA_RABBITMQ_URL` | RabbitMQ 连接串（含凭证） | api-server, worker |
| `CLOUD_MEDIA_OBJECT_STORAGE_ACCESS_KEY_ID` | 对象存储 Access Key | api-server, worker |
| `CLOUD_MEDIA_OBJECT_STORAGE_SECRET_ACCESS_KEY` | 对象存储 Secret Key | api-server, worker |

**使用此 Secret 的 Deployment**:
- ✅ apps/api-server/deployment.yaml
- ✅ apps/worker/deployment.yaml

---

### 5. KEDA (keda/)

**Secret 名称**: `keda-rabbitmq-secret`

| 密钥 | 说明 | 引用方 |
|------|------|--------|
| `host` | RabbitMQ 连接串（含凭证） | trigger-authentication.yaml → ScaledObject |

**引用此 Secret 的资源**:
- ✅ keda/trigger-authentication.yaml

---

### 6. Observability (observability/)

#### 6.1 Loki

**Secret 名称**: `minio-credentials`

| 环境变量 / 密钥 | 说明 | StatefulSet | ConfigMap |
|----------------|------|------------|-----------|
| `MINIO_ACCESS_KEY_ID` | S3 Access Key | ✅ loki/statefulset.yaml | ✅ loki/configmap.yaml |
| `MINIO_SECRET_ACCESS_KEY` | S3 Secret Key | ✅ loki/statefulset.yaml | ✅ loki/configmap.yaml |

#### 6.2 Tempo

**Secret 名称**: `minio-credentials`

| 环境变量 / 密钥 | 说明 | StatefulSet | ConfigMap |
|----------------|------|------------|-----------|
| `MINIO_ACCESS_KEY_ID` | S3 Access Key | ✅ tempo/statefulset.yaml | ✅ tempo/configmap.yaml |
| `MINIO_SECRET_ACCESS_KEY` | S3 Secret Key | ✅ tempo/statefulset.yaml | ✅ tempo/configmap.yaml |

#### 6.3 Mimir

**Secret 名称**: `minio-credentials`

| 环境变量 / 密钥 | 说明 | StatefulSet | ConfigMap |
|----------------|------|------------|-----------|
| `MINIO_ACCESS_KEY_ID` | S3 Access Key | ✅ mimir/statefulset.yaml | ✅ mimir/configmap.yaml |
| `MINIO_SECRET_ACCESS_KEY` | S3 Secret Key | ✅ mimir/statefulset.yaml | ✅ mimir/configmap.yaml |

#### 6.4 Grafana

**Secret 名称**: `grafana-credentials`

| 环境变量 / 密钥 | 说明 | Deployment |
|----------------|------|------------|
| `GF_SECURITY_ADMIN_USER` | 管理员用户名 | ✅ grafana/deployment.yaml |
| `GF_SECURITY_ADMIN_PASSWORD` | 管理员密码 | ✅ grafana/deployment.yaml |

---

## 使用流程

### 首次设置

```bash
# 1. 确保 kubeseal 已安装
kubeseal --version

# 2. 确保可以连接到 Kubernetes 集群
kubectl cluster-info

# 3. 运行加密脚本（一次性处理所有 Secret）
./scripts/seal-secrets.sh

# 脚本会自动：
#   - 递归扫描 k8s/ 下所有 secrets.unsealed.yaml
#   - 加密为同目录下的 sealed-secrets.yaml
#   - 更新同目录的 kustomization.yaml
#   - 备份私钥
```

### 修改密钥

```bash
# 1. 编辑对应目录下的 secrets.unsealed.yaml
# 2. 重新运行加密脚本
./scripts/seal-secrets.sh
# 3. 提交加密后的文件
git add k8s/**/sealed-secrets.yaml
git add k8s/**/kustomization.yaml
```

---

## 文件安全清单

| 文件 | 可提交 Git？ | 说明 |
|------|-----------|------|
| `**/secrets.unsealed.yaml` | ❌ 否 | 未加密的明文 Secret |
| `**/sealed-secrets.yaml` | ✅ 是 | 加密后的 SealedSecret |
| `sealed-secrets-public-key.pem` | ✅ 是 | 公钥（用于加密） |
| `sealed-secrets-private-key.backup.yaml` | ❌ 否 | 私钥备份（需妥善保管） |

