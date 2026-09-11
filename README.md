# heimdall-devops-core

CLI interna em Go para diagnóstico e correção de problemas de DevOps:
Terraform, pipelines Azure DevOps e recursos Azure.

O foco é o ciclo de investigação real: a pipeline quebrou, o log não diz o
suficiente, e você precisa olhar o plano ou o state antes de agir. Os comandos
são de leitura por padrão e explicitam contra o que estão operando.

## Requisitos

- Go 1.24 ou superior
- `terraform` — só para gerar a entrada do `plan-review`
- Azure CLI (`az`) autenticado — só para os comandos que leem o Azure

## Build

```sh
go build -o heimdall ./cmd/heimdall
```

```sh
go install github.com/cadiguni/heimdall-devops-core/cmd/heimdall@latest
```

## Comandos

### `terraform plan-review`

Analisa a saída de `terraform show -json` e destaca o que o plano destrói,
recria ou remove do state.

```sh
terraform plan -out=tf.plan
terraform show -json tf.plan > plan.json
heimdall terraform plan-review --plan-json plan.json
```

```
Plano gerado por Terraform v1.14.5

Resumo (recursos gerenciados)
  criar     1
  alterar   1
  destruir  1
  recriar   1

Operações destrutivas (2)
  destruir  terraform_data.to_delete   (sem bloco de configuração correspondente)
  recriar   terraform_data.to_replace  (atributo alterado não suporta update in-place)
```

O plano também pode vir pelo stdin (`--plan-json -`), e a saída pode ser JSON
(`--output json`) para ser consumida por outra ferramenta.

**Serve como gate de pipeline.** Os códigos de saída separam os motivos:

| Código | Significado |
| --- | --- |
| 0 | nada a apontar |
| 1 | erro de execução — o heimdall falhou |
| 2 | operação destrutiva encontrada |
| 3 | plano incompleto (`-target` ou mudanças adiadas) |

O código 3 existe porque um plano que não cobre toda a configuração não serve
como gate: a destruição pode estar justamente no que ficou de fora. Planos de
Terraform anterior a 1.8 não informam se estão completos, e nesses casos o
código 3 nunca dispara.

Os dois gates podem ser desligados com `--fail-on-destroy=false` e
`--fail-on-incomplete=false`. Quando as duas condições valem ao mesmo tempo, a
destruição tem precedência no código de saída.

**Nenhum valor de atributo é impresso** — só endereço, tipo e operação. Um
plano real carrega senhas e referências de Key Vault em `before`/`after`, então
a saída pode ir para o log de uma pipeline sem vazar secrets.

### `terraform states list`

Lista os arquivos de state de um container de Blob Storage, com data de
modificação, tamanho e estado do lock. Não roda `terraform init` e não baixa o
state.

```sh
heimdall terraform states list --account stterraform --container time1
heimdall terraform states list --account stterraform --container time1 --prefix prod/
```

```
Container: https://stterraform.blob.core.windows.net/time1

AMBIENTE  STATE                MODIFICADO (UTC)  TAMANHO   LOCK
dev       dev/app.tfstate      2026-09-10 11:00  4.0 KB    livre
prod      prod/app.tfstate     2026-09-10 11:00  117.2 KB  TRAVADO

States travados (1)
  prod/app.tfstate — OperationTypePlan, por lucas@vm-build, há 3h12min, id 4a1e6bd4
```

O ambiente vem do primeiro segmento do caminho, seguindo a convenção de uma
pasta por ambiente dentro do container do time.

**Sobre o lock.** O backend `azurerm` trava o state com um lease infinito no
blob e guarda os dados de quem travou na metadata `terraformlockid`, em base64.
O comando lê os dois. Metadata de lock **sem** lease ativo é reportada como
órfã, não como lock: o Terraform limpa a metadata ao destravar, então sobra
costuma ser resquício de execução interrompida — não é caso de `force-unlock`.

**Autenticação.** Usa `DefaultAzureCredential`, que resolve nesta ordem
variáveis de ambiente, identidade gerenciada e a sessão do `az login`. Não há
opção de access key de propósito: chave passada em flag vaza no histórico do
shell, em `ps` e no log da pipeline.

Autenticar por Entra ID exige role de *data plane* — `Storage Blob Data Reader`
no container ou na account. Quem normalmente usa access key pode não ter essa
role atribuída ao próprio usuário; o sintoma é **403 na listagem**, não erro de
login.

## Desenvolvimento

```sh
go test ./...
go vet ./...
gofmt -l .
```

As fixtures de plano em `internal/terraform/testdata/` são saídas reais de
`terraform show -json`, não JSON escrito à mão. O
[README de testdata](internal/terraform/testdata/README.md) explica como
regenerá-las — usam `terraform_data`, que é embutido no Terraform e não exige
provider externo nem credencial de nuvem.

O acesso ao Azure fica atrás da interface `azure.BlobLister`, então a lógica
que classifica states é testada com um lister falso, sem nuvem. O adapter do
SDK é a única parte que fala com o Azure de verdade, e é deliberadamente fina.

O repositório versiona tudo em LF (ver `.gitattributes`). Sem isso, um clone em
Windows com `core.autocrlf=true` deixa a cópia de trabalho em CRLF e o `gofmt`
acusa todo arquivo como mal formatado.

## Estado atual

| Módulo | Situação |
| --- | --- |
| `terraform plan-review` | funcionando, coberto por testes |
| `terraform states list` | funcionando, verificado contra um container real com 57 states |
| Pipeline Doctor | não começou |

O caminho de lock do `states list` só foi exercitado por teste unitário: não
havia nenhum state travado no momento da verificação. A leitura de metadata
`terraformlockid` em um lock real continua por confirmar.

Convenções e princípios do projeto estão em [CLAUDE.MD](CLAUDE.MD).
