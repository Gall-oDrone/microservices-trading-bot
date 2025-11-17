### EKS Migration Plan (Concise, Phased)

Goal: Migrate to Amazon EKS with a minimal production-ready baseline, enabling clean deployments of services in later stages.

References
- Data on EKS (patterns, addons, ops): [awslabs/data-on-eks](https://github.com/awslabs/data-on-eks?tab=readme-ov-file)
- AI on EKS (IRSA, GitOps, observability examples): [awslabs/ai-on-eks](https://github.com/awslabs/ai-on-eks)
- Terraform EKS Blueprints (modules, addons, GitOps): [aws-ia/terraform-aws-eks-blueprints](https://github.com/aws-ia/terraform-aws-eks-blueprints)

Scope of this phase
- Infrastructure only. No application rollout yet.
- Baseline ready for production: private networking, managed addons, IRSA, ingress, metrics.

Phase 1 – Foundations (Dev)
- Terraform state, AWS provider, versions pinning.
- VPC with 2–3 AZs: private subnets for nodes, public for ingress, NAT for egress.
- EKS cluster v1.33: IRSA enabled; managed addons (VPC CNI, CoreDNS, kube-proxy, EBS CSI).
- ECR repositories for services.
- IAM OIDC provider + base IRSA roles for controllers (placeholders narrowed later).

Phase 2 – Platform Essentials
- Helm-installed addons:
  - AWS Load Balancer Controller (ALB) with IRSA.
  - Metrics Server (already added in dev).
  - ExternalDNS and Cert-Manager (next step) with Route53 zone + DNS01 solver.
- ServiceAccount annotations (IRSA) pattern documented for workloads.

Phase 3 – Data Plane Dependencies
- Managed Kafka (Amazon MSK) to back existing Kafka usage.
- ElastiCache Redis. Optionally RDS for relational needs.
- Wire secrets/config with External Secrets Operator (AWS Secrets Manager/SSM).

Phase 4 – CI/CD Integration
- GitHub OIDC to AWS (assume-role) – no static creds.
- Build/push images to ECR per service.
- Deploy to EKS dev via Kustomize or GitOps (Argo CD optional follow-up).

Phase 5 – Hardening & Observability
- NetworkPolicies, PodSecurity standards, resource requests/limits, HPAs.
- Prometheus/Grafana or AMP/AMG integration; ALB and autoscaling dashboards.
- Backup/restore strategy for any stateful data plane components.

Implemented
- kube-prometheus-stack (Prometheus + Grafana) via Helm in `monitoring` namespace.
  - Grafana/Prometheus as ClusterIP for internal access only (can expose via Ingress later).
Next
- Enforce baseline NetworkPolicies (already present in `security/network-policies/`).
- Consider PodSecurity admission standards (baseline/restricted) at namespace level.

Phase 6 – Promote to Staging/Prod
- Replicate env with right sizing, QoS, quotas.
- Gradual rollout (blue/green, canary) using Ingress or Argo Rollouts (optional).

Current repo implementation
- Modules scaffolded under `infrastructure/terraform/modules`: `vpc`, `eks`, `iam`, `ecr`.
- `envs/development` wires VPC + EKS + ECR + IRSA and installs Metrics Server.
- ALB Ingress Controller added with IRSA.

Next steps (short list)
- Add ExternalDNS + Cert-Manager (Route53), External Secrets Operator.
- Add Karpenter or tune managed node groups; spot/on-demand mix.
- Prepare GitHub Actions OIDC + ECR push + Kustomize deploy workflow.
