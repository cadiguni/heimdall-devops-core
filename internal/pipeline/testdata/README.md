# Fixtures de log de pipeline

`pipeline_log_misto.txt` é um log sintético que concatena **11 falhas reais**
de pipelines de Terraform no Azure DevOps, cada uma com o trecho como saiu no
log original.

## Higienização

O log original não continha secret nenhum (nem token, nem access key, nem
connection string), mas continha identificadores. Foram substituídos de forma
determinística:

| Original | Na fixture |
| --- | --- |
| 4 subscription IDs | `00000000-0000-0000-0000-00000000000{1..4}` |
| e-mail em tag de recurso | `pessoa@example.com` |
| domínio da empresa | `example.com` |
| 4 nomes de aplicação | `appalpha`, `appbeta`, `appgama`, `aplicacaodemo01` |

`aplicacaodemo01` tem exatamente o mesmo comprimento do nome original de
propósito: o caso do Storage Account depende de o nome final ter 26 caracteres
para estourar o limite de 24 do Azure. Um substituto mais curto tornaria o
nome válido e o caso deixaria de testar o que deveria.

As linhas de anotação (`erro N - <diagnóstico>`) do arquivo original foram
removidas — a fixture é log puro. O diagnóstico de cada caso virou a tabela
abaixo.

## Os 11 casos

| # | Falha | Assinatura |
| --- | --- | --- |
| 1 | CDN custom domain não destrói: CNAME ainda aponta para o endpoint | `cdn-custom-domain-cname` |
| 2 | `-var` de variável não declarada no módulo raiz | `undeclared-variable` |
| 3 | Resource Group já existe no Azure, fora do state | `resource-already-exists` |
| 4 | Data source aponta para RG que não existe (criado à mão só em prod) | `resource-group-not-found` |
| 5 | Input obrigatório da task do Azure DevOps vazio | `missing-pipeline-input` |
| 6 | Nome de Storage Account com 26 caracteres | `invalid-storage-account-name` |
| 7 | Seis `-var` de variáveis não declaradas na mesma execução | `undeclared-variable` |
| 8 | Código usa schema de AzureRM anterior ao provider em uso | `provider-schema-mismatch` |
| 9 | 404 `ParentResourceNotFound` em Application Insights | `parent-resource-not-found` |
| 10 | Log Analytics Workspace já existe, precisa de import | `resource-already-exists` |
| 11 | Data source de App Service não encontrado por atraso de criação | `azure-resource-not-found` |

## O que não virou assinatura

`##[error]Error: TerraformPlanFailed`, `failed with exit code 1` e
`Releasing state lock` aparecem em praticamente toda falha e não apontam para
causa nenhuma. Reconhecê-los só encheria o relatório de ruído.

Não há nenhum caso de **state lock preso** neste corpus. A assinatura
correspondente não foi escrita justamente por isso: sem um log real, a
mensagem e a extração seriam chute.

## Logs novos

Logs crus vão em `logs-raw/` na raiz do repositório, que é ignorado pelo git.
Nada de lá entra em commit sem passar pela higienização acima.
