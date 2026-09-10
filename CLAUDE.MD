# heimdall-devops-core

CLI interna em Go para automatizar diagnóstico e correção de problemas de
DevOps: Terraform (drift, import, migração de state), pipelines Azure DevOps,
templates de pipeline compartilhados, Variable Groups e recursos Azure.

## Arquitetura
- `cmd/heimdall/` — entrypoint da CLI (Cobra)
- `internal/core/` — lógica de negócio compartilhada, sem I/O direto
- `internal/terraform/` — módulo Terraform Doctor (usa terraform-exec/terraform-json)
- `internal/pipeline/` — módulo Pipeline Doctor
- `internal/azure/` — integrações Azure SDK for Go
- Futuro: `cmd/heimdall-server/` reaproveitando os mesmos pacotes `internal/`
  para expor um portal via HTTP.

## Princípios (não negociáveis)
- Nunca editar `.tfstate` em JSON diretamente — sempre `terraform state mv`,
  `import`/`moved` blocks.
- Toda operação potencialmente destrutiva precisa de dry-run por padrão;
  execução real exige flag explícita (`--apply` ou similar).
- Nunca logar secrets em texto puro (Variable Groups, Key Vault refs).
- MVP: focar em `terraform plan-review` antes de qualquer outro módulo.

## Convenções
- Go 1.24+, Cobra para subcomandos.
- Testes com fixtures reais de `terraform show -json` em `testdata/`.
- Scripts bash/PowerShell existentes podem ser chamados via `os/exec` como
  ponte temporária até o módulo Go equivalente estar pronto.