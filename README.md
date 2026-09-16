# heimdall-devops-core

[![CI](https://github.com/cadiguni/heimdall-devops-core/actions/workflows/ci.yml/badge.svg)](https://github.com/cadiguni/heimdall-devops-core/actions/workflows/ci.yml)

CLI interna em Go para diagnóstico e correção de problemas de DevOps:
Terraform, pipelines Azure DevOps e recursos Azure.

O foco é o ciclo de investigação real: a pipeline quebrou, o log não diz o
suficiente, e você precisa olhar o plano ou o state antes de agir. Os comandos
são de leitura por padrão e explicitam contra o que estão operando.

## Requisitos

- Go 1.25 ou superior (exigido pelo Azure SDK e pelo terraform-exec)
- `terraform` — só para gerar a entrada do `plan-review`
- Azure CLI (`az`) autenticado — só para os comandos que leem o Azure

## Build

A partir do repositório clonado:

```sh
go install ./cmd/heimdall
```

Ou, para só gerar o binário no diretório atual:

```sh
go build -o heimdall ./cmd/heimdall
```

**Instale sempre do seu checkout, não do proxy.** Este repositório não tem tag
de versão, então `go install github.com/cadiguni/heimdall-devops-core/cmd/heimdall@latest`
não pega o commit mais novo do `main`: pega o último pseudo-version que o
`proxy.golang.org` indexou, que fica atrás por um tempo depois de um push. O
sintoma é um comando novo simplesmente não existir no binário instalado.

Para conferir o que está instalado:

```sh
heimdall terraform states --help
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
| 4 | drift, só com `--fail-on-drift` |

O código 3 existe porque um plano que não cobre toda a configuração não serve
como gate: a destruição pode estar justamente no que ficou de fora. Planos de
Terraform anterior a 1.8 não informam se estão completos, e nesses casos o
código 3 nunca dispara.

Os dois gates podem ser desligados com `--fail-on-destroy=false` e
`--fail-on-incomplete=false`. Quando as duas condições valem ao mesmo tempo, a
destruição tem precedência no código de saída.

**Drift.** O JSON do plano traz duas coisas diferentes: o que o plano vai fazer
e o que já mudou no provedor por fora do Terraform desde o último apply. A
segunda aparece em seção própria:

```
Mudou fora do Terraform (1)
  sumiu  azurerm_storage_account.stg

O apply vai reverter isso. Se a mudança manual era intencional,
ela precisa entrar na configuração antes.
```

É o que faz um plano surpreender: sem isso, um "update" na lista de mudanças
parece rotina quando na verdade é o Terraform desfazendo algo que alguém mexeu
no portal.

Drift é relatado sempre, mas **só reprova com `--fail-on-drift`**, desligado
por padrão — em time onde mexer no portal é rotina, reprovar por padrão
inviabilizaria a revisão. Na precedência dos códigos de saída ele fica por
último: destrutiva (2), depois plano incompleto (3), depois drift (4).

**Nenhum valor de atributo é impresso** — só endereço, tipo e operação. Um
plano real carrega senhas e referências de Key Vault em `before`/`after`, e as
entradas de drift carregam os mesmos valores; nenhuma das duas sai na saída.

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

**Irregularidades.** A listagem também aponta states com nome ou tamanho
suspeito, cada um deles normalmente um `terraform init` que criou state órfão
em vez de usar o certo:

| Irregularidade | O que é |
| --- | --- |
| `unexpanded-variable` | `$(…)` ou `${…}` sobrou no nome do blob |
| `backend-key-prefix` | o `key=` do `-backend-config` virou parte do caminho |
| `dangling-separator` | nome termina em `-` ou `_` antes da extensão |
| `possibly-empty` | tamanho compatível com state sem recursos |

O limite de `possibly-empty` é 300 bytes, e não é chute: um state v4 recém-criado
sem recursos tem 181 bytes medidos, e um com dois recursos simples já passa de
1500. Quem confirma é o `states show`.

### `terraform states show`

Baixa um state do container e resume o que ele rastreia.

```sh
heimdall terraform states show prod/app.tfstate --account stterraform --container time1
```

```
State      finops/finopsplatform.tfstate
Terraform  v1.9.3
Serial     147
Recursos   27 gerenciados, 1 data sources
Outputs    cosmos_endpoint, function_app_name, servicebus_connection_string

ENDEREÇO                                       PROVIDER
azurerm_cosmosdb_account.finops                registry.terraform.io/hashicorp/azurerm
azurerm_linux_function_app.finops              registry.terraform.io/hashicorp/azurerm
```

Endereços vêm remontados como o Terraform os escreve, incluindo prefixo de
módulo e chave de `count`/`for_each` — prontos para colar em um `terraform
import` ou `state mv`.

**Imprime apenas endereço, tipo e provider, nunca valor de atributo.** Um state
guarda atributos em texto puro, e senha e connection string moram ali. Dos
outputs sai só o nome, pelo mesmo motivo — note no exemplo acima que existe um
output chamado `servicebus_connection_string` cujo valor não é impresso.

**Autenticação.** Usa `DefaultAzureCredential`, que resolve nesta ordem
variáveis de ambiente, identidade gerenciada e a sessão do `az login`. Não há
opção de access key de propósito: chave passada em flag vaza no histórico do
shell, em `ps` e no log da pipeline.

Autenticar por Entra ID exige role de *data plane* — `Storage Blob Data Reader`
no container ou na account. Quem normalmente usa access key pode não ter essa
role atribuída ao próprio usuário; o sintoma é **403 na listagem**, não erro de
login.

### `terraform states rm` e `states mv`

Cirurgia de state: tirar um recurso do rastreamento, ou trocar seu endereço.
Também aceitam `state` no singular, como o terraform escreve.

```sh
heimdall terraform state rm azurerm_resource_group.rg \
  --account stterraform --container time1 --key dev/app.tfstate

heimdall terraform state mv azurerm_storage_account.stg module.app.azurerm_storage_account.stg \
  --account stterraform --container time1 --key dev/app.tfstate
```

```
Verificações
  [ok  ] state existe: finops/finopsplatform.tfstate, 85565 bytes
  [ok  ] state destravado: sem lease ativo
  [ok  ] endereço existe: azurerm_cosmosdb_sql_container.audit_logs está no state

Sai do state (1)
  azurerm_cosmosdb_sql_container.audit_logs  registry.terraform.io/hashicorp/azurerm

Dry-run: nada foi executado.

A infraestrutura no Azure continua existindo — só deixa de ser rastreada.
```

As verificações são o espelho das do `import`: lá o endereço precisa estar
**livre**, aqui precisa **existir**. No `mv`, o destino é que precisa estar
livre — mover para um endereço ocupado sobrescreveria o que estiver lá.

**A lista "Sai do state" não é enfeite.** Um endereço sem índice atinge todas
as instâncias de um `count` ou `for_each`: pedir `rm azurerm_subnet.sub` quando
existem `sub["app"]` e `sub["db"]` tira as duas. O preflight avisa e lista
cada uma antes de qualquer confirmação.

Dry-run por padrão, `--apply` para executar, e reprovação sai com código 2 sem
sugerir comando nenhum.

### `terraform import`

Importa um recurso existente no Azure para o state, depois de verificar que a
operação tem chance de dar certo e que é no state pretendido.

```sh
heimdall terraform import azurerm_resource_group.rg \
  /subscriptions/.../resourceGroups/app-dev-rg \
  --account stterraform --container time1 --key dev/app.tfstate
```

```
Alvo
  container  https://stterraform.blob.core.windows.net/time1
  state      dev/app.tfstate
  ambiente   dev
  endereço   azurerm_resource_group.rg
  recurso    /subscriptions/.../resourceGroups/app-dev-rg

Verificações
  [ok  ] state existe: dev/app.tfstate, 85565 bytes
  [ok  ] state destravado: sem lease ativo
  [ok  ] endereço livre: azurerm_resource_group.rg não está no state
  [ok  ] conteúdo do state: 27 recursos gerenciados, 1 data sources

Dry-run: nada foi executado.
```

**Dry-run por padrão.** Sem `--apply`, só verifica e mostra os comandos
equivalentes. As verificações vêm dos modos de falha reais:

| Verificação | Por quê |
| --- | --- |
| state existe | se não existe, a chave do backend está errada |
| state destravado | com lock ativo o import esperaria e estouraria |
| endereço livre | se já está no state, import é a operação errada |
| conteúdo do state | state vazio avisa: pode ser o backend errado |

Reprovou, sai com código 2 e não sugere comando nenhum — sugerir seria convidar
a contornar a checagem.

**O `terraform init` nunca é executado pelo heimdall**, nem com `--apply`. O
diretório (`--chdir`) precisa já estar inicializado contra o backend certo;
reconfigurar backend de diretório alheio pode migrar state, e isso não cabe num
comando cuja proposta é ser seguro. O `init` equivalente aparece no dry-run para
você rodar.

**Se você usa Git Bash ou MSYS**, prefixe com `MSYS_NO_PATHCONV=1`:

```sh
MSYS_NO_PATHCONV=1 heimdall terraform import azurerm_resource_group.rg /subscriptions/...
```

Esses shells convertem argumento iniciado em `/` para caminho do Windows, e todo
id de recurso do Azure começa com `/subscriptions/` — o id chegaria como
`C:/Program Files/Git/subscriptions/...`. O comando detecta e recusa com essa
explicação, em vez de deixar o provider falhar sem dizer por quê.

### `pipeline diagnose`

Lê um log de pipeline e aponta as falhas que reconhece, com causa provável e o
que fazer. Quando o log traz dado suficiente, monta o comando de correção.

```sh
heimdall pipeline diagnose --log build.log
terraform plan 2>&1 | heimdall pipeline diagnose --log -
```

```
21 achado(s): 19 erro(s), 2 aviso(s)

[erro] Recurso já existe no Azure, fora do state  (resource-already-exists)
  Causa: O recurso existe no Azure mas o state não o conhece, então o Terraform
  tenta criar de novo e o Azure recusa.
  O que fazer: Importar o recurso para o state, ou remover do Azure antes de recriar.
  Ocorrências (2):
    linha 63: Error: a resource with the ID "/subscriptions/…" already exists…
      address=azurerm_resource_group.rg[0]  resource_id=/subscriptions/…
  Comandos sugeridos (confira antes de rodar):
    terraform import 'azurerm_resource_group.rg[0]' '/subscriptions/…'
```

Ocorrências da mesma assinatura são agrupadas: seis variáveis não declaradas na
mesma execução são um problema, não seis.

O catálogo atual reconhece dez assinaturas, todas derivadas de falhas reais:

| Assinatura | O que é |
| --- | --- |
| `resource-already-exists` | recurso existe no Azure, fora do state — gera o `terraform import` |
| `undeclared-variable` | `-var` para variável que o módulo raiz não declara |
| `provider-schema-mismatch` | código usa bloco/argumento que a versão do provider não tem |
| `cdn-custom-domain-cname` | custom domain não destrói porque o CNAME ainda aponta |
| `resource-group-not-found` | data source aponta para RG inexistente |
| `azure-resource-not-found` | data source consultou recurso que não existe |
| `parent-resource-not-found` | 404 `ParentResourceNotFound` |
| `invalid-storage-account-name` | nome fora das regras do Azure — diz qual regra quebrou |
| `missing-pipeline-input` | input obrigatório da task do Azure DevOps vazio |
| `deprecated-argument` | aviso: vai virar erro no próximo major do provider |

**Nada é executado.** Os comandos sugeridos são impressos para você conferir e
rodar. E o comando sempre sai com 0 quando funciona: diagnosticar não é
reprovar, então não há gate aqui.

Erros que são só consequência (`TerraformPlanFailed`, `failed with exit code 1`)
ficam fora do catálogo de propósito: aparecem em toda falha e não apontam para
causa nenhuma.

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
| `terraform states list` | funcionando, verificado contra um container real com 57 states; aponta 4 tipos de irregularidade |
| `terraform states show` | funcionando, verificado contra um state real de 27 recursos |
| `terraform import` | preflight verificado contra state real; o `--apply` tem teste de integração com terraform e backend local, mas ainda não rodou contra o Azure |
| `terraform states rm` / `mv` | preflight verificado contra state real; `--apply` com teste de integração em backend local |
| `pipeline diagnose` | funcionando, 10 assinaturas cobrindo um corpus de 11 falhas reais |

Duas lacunas conhecidas:

- A leitura da metadata `terraformlockid` do `states list` só tem teste
  unitário. Na verificação contra um lease real o blob tinha sido travado por
  fora do Terraform, então não havia metadata para ler.
- O catálogo de assinaturas não cobre **state lock preso**. Não há nenhum caso
  desses no corpus, e escrever a assinatura sem um log real seria chute.

Convenções e princípios do projeto estão em [CLAUDE.MD](CLAUDE.MD).
