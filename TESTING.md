# Como testar

Dois níveis independentes: a **suíte automatizada**, que não toca em nuvem
nenhuma, e a **verificação manual** de cada comando contra um ambiente de
verdade.

Comece pela suíte. Ela é rápida e pega quebra de código; a verificação manual
existe para confirmar o que só aparece contra o serviço real.

---

## 1. Suíte automatizada

### Pré-requisitos

| O quê | Para quê | Sem isso |
| --- | --- | --- |
| Go 1.25+ | tudo | nada roda |
| `terraform` no PATH | 6 testes de integração | eles se marcam como *skip*, o resto passa |

**Não precisa de Azure, nem de `az login`, nem de rede.** Todo acesso a serviço
está atrás de interface e é exercitado com dublê.

### Rodar

```sh
go build ./...
go vet ./...
gofmt -l .
go test ./... -count=1
```

O `gofmt -l .` deve sair **vazio**. Se listar arquivos, ou o código está
desformatado, ou seu checkout veio em CRLF — veja a seção de problemas comuns.

Com cobertura:

```sh
go test ./internal/... ./cmd/... -cover -count=1
```

Referência do que é normal hoje: 345 casos passando, cobertura de 93% em
`internal/core`, 91% em `internal/pipeline`, 88% em `internal/terraform`, 85% em
`internal/azure` e 70% em `cmd/heimdall`.

### A armadilha do *skip*

Seis testes executam o `terraform` de verdade (em backend local, sem nuvem) e se
marcam como *skip* quando o binário não está no PATH. **Skip conta como
sucesso**, então uma suíte "verde" sem terraform não provou o caminho de escrita.

Para conferir que eles rodaram mesmo:

```sh
go test ./internal/terraform/ -run "ExecImporter|ExecState" -v -count=1
```

Se aparecer `SKIP`, instale o terraform antes de confiar no resultado. O CI faz
essa checagem automaticamente e reprova o build se o teste pular.

---

## 2. Preparar o ambiente para a verificação manual

### Autenticação

Todos os comandos que falam com Azure ou Azure DevOps usam a identidade do
ambiente. Localmente:

```sh
az login
```

Não existe opção de access key nem de PAT em nenhum comando. Se o `az` não
estiver no PATH da sessão onde você roda o heimdall, a autenticação falha — em
Windows, o instalador só atualiza o PATH de shells abertos depois dele.

### Permissões

| Comando | Precisa de |
| --- | --- |
| `terraform states *`, `terraform import` | role de *data plane* no Storage: `Storage Blob Data Reader` para ler, `Storage Blob Data Contributor` para o `--apply` |
| `pipeline variables`, `pipeline connections` | leitura dos Variable Groups / service connections do projeto |
| `terraform plan-review`, `pipeline diagnose` | nada — leem arquivo local |

Quem usa access key no dia a dia frequentemente **não tem** a role de data plane
atribuída ao próprio usuário. O sintoma é **403 na listagem**, não erro de login.

### Perfil (opcional, mas poupa digitação)

Crie um `.heimdall.yaml` no diretório onde você trabalha:

```yaml
profiles:
  meutime:
    descricao: backend de Terraform do time
    account: minhastorageaccount
    container: meutime

  meutime-dev:
    account: minhastorageaccount
    container: meutime
    key: dev/app.tfstate

  devops:
    org: minhaorg
    project: MeuProjeto
```

Nada de segredo vai nele. Campo desconhecido é recusado, então um `acccount`
digitado errado aparece na hora.

---

## 3. Verificar os comandos que só leem

Nenhum deles altera nada. Pode rodar em qualquer ambiente, inclusive produção.

### `pipeline diagnose` — não precisa de nuvem

O mais fácil de começar, porque só lê um arquivo:

```sh
heimdall pipeline diagnose --log internal/pipeline/testdata/pipeline_log_misto.txt
```

Esperado: 21 achados em 10 assinaturas, e dois comandos `terraform import`
montados com endereço e ID preenchidos.

Com um log seu:

```sh
heimdall pipeline diagnose --log logs-raw/meu-log.txt
```

`logs-raw/` é ignorado pelo git de propósito — log de pipeline costuma carregar
subscription ID e, às vezes, segredo que caiu no stdout.

### `terraform plan-review` — precisa de terraform, não de nuvem

```sh
terraform plan -out=tf.plan
terraform show -json tf.plan > plan.json
heimdall terraform plan-review --plan-json plan.json
echo "código de saída: $?"
```

Os códigos importam: **0** nada a apontar, **2** destruição, **3** plano
incompleto, **4** drift (só com `--fail-on-drift`). Para testar sem gerar plano,
use as fixtures:

```sh
heimdall terraform plan-review --plan-json internal/terraform/testdata/plan_mixed.json                 # sai 2
heimdall terraform plan-review --plan-json internal/terraform/testdata/plan_no_changes.json            # sai 0
heimdall terraform plan-review --plan-json internal/terraform/testdata/plan_targeted_incomplete.json   # sai 3
heimdall terraform plan-review --plan-json internal/terraform/testdata/plan_com_drift.json --fail-on-drift  # sai 4
```

### `terraform states list` — inventário do container

```sh
heimdall terraform states list --account minhastorageaccount --container meutime
```

Confira três coisas na saída: o cabeçalho diz o container certo, a coluna LOCK
reflete a realidade, e a seção de irregularidades aponta o que você reconhece
como problema.

### `terraform states show` — conteúdo de um state

```sh
heimdall terraform states show dev/app.tfstate --profile meutime
```

Baixa o state. **Nenhum valor de atributo é impresso**, só endereço, tipo e
provider — mas o arquivo em si passa pela memória do processo, então trate como
leitura de dado sensível: não redirecione para log compartilhado.

### `pipeline variables` e `pipeline connections`

```sh
heimdall pipeline variables list --org minhaorg --project MeuProjeto
heimdall pipeline connections list --org minhaorg --project MeuProjeto
```

O `variables list` não lê valor nenhum, só contagens. O `variables show <grupo>`
lista nomes; valor só com `--show-values`, e **nunca** o de variável secreta.

```sh
# não use isto em log de pipeline
heimdall pipeline variables show meugrupo --profile devops --show-values
```

---

## 4. Verificar os comandos que escrevem

São três: `terraform import`, `terraform states rm` e `terraform states mv`.
Todos são **dry-run por padrão** e só executam com `--apply`.

### Ordem segura

1. **Sempre o dry-run primeiro**, sem `--apply`. Leia o bloco "Alvo" e confirme
   container, state e ambiente antes de qualquer coisa.
2. **Em dev antes de qualquer outro ambiente.** Não existe comando que impeça
   você de apontar para produção; o que existe é o cabeçalho dizendo para onde.
3. O diretório de `--chdir` precisa estar **já inicializado** contra o backend
   certo. O heimdall nunca roda `terraform init` — ele mostra o comando
   equivalente no dry-run, e rodar é seu.

### Dry-run

```sh
heimdall terraform import azurerm_resource_group.rg \
  /subscriptions/.../resourceGroups/app-dev-rg \
  --profile meutime-dev
```

```sh
heimdall terraform states rm azurerm_resource_group.rg --profile meutime-dev
heimdall terraform states mv azurerm_storage_account.stg module.app.azurerm_storage_account.stg --profile meutime-dev
```

Reprovou o preflight, sai com **código 2** e não imprime comando nenhum. Os
motivos possíveis:

| Verificação | Reprova quando |
| --- | --- |
| state existe | a chave do backend está errada |
| state destravado | tem lease ativo — o terraform ficaria esperando |
| endereço livre (import) | o recurso já está no state |
| endereço existe (rm/mv) | não há o que remover ou mover |
| destino livre (mv) | sobrescreveria o que está lá |

No `rm`, confira a lista **"Sai do state"** antes de prosseguir: endereço sem
índice atinge todas as instâncias de um `count`/`for_each`.

### Executar

Só depois do dry-run limpo:

```sh
heimdall terraform states rm azurerm_resource_group.rg --profile meutime-dev --apply
```

Existe um intervalo entre o preflight e a execução em que outra pessoa pode
travar o state. Nesse caso quem reclama é o terraform, com a mensagem dele.

---

## 5. Problemas comuns

**`gofmt -l .` lista todos os arquivos.** Sua cópia de trabalho está em CRLF. O
repositório é versionado em LF (`.gitattributes`), mas com `core.autocrlf=true`
no Windows um clone antigo pode ter ficado em CRLF — e aí o `gofmt` acusa todo
arquivo, mesmo com o conteúdo commitado correto.

Para confirmar que é só isso, veja se o `gofmt` reclama apenas do `^M`:

```sh
gofmt -d cmd/heimdall/main.go | cat -A | head
```

Para corrigir, **com a árvore limpa** (o comando reescreve arquivos a partir do
git):

```sh
git add --renormalize .
```

Se depois disso o `gofmt` continuar reclamando, o índice foi normalizado mas a
cópia de trabalho não. Aí apague os arquivos afetados e restaure do git:

```sh
for f in $(git ls-files); do file "$f" | grep -q CRLF && rm "$f"; done
git checkout -- .
```

**`unknown flag: --account` num subcomando que deveria existir.** Binário
desatualizado. Confira e reinstale do checkout:

```sh
heimdall --version
go install ./cmd/heimdall
```

Se o `--version` termina em `+dirty`, o binário foi construído com alteração não
commitada.

**403 ao listar blobs.** Falta role de *data plane* (`Storage Blob Data Reader`),
não é problema de login.

**"o Azure DevOps devolveu a página de login".** É o HTTP 203: o token não tem o
escopo certo. Normalmente a assinatura do `az login` não está no tenant ligado à
organização.

**O ID do recurso virou `C:/Program Files/Git/subscriptions/...`.** Git Bash e
MSYS convertem argumento que começa com `/`. Prefixe:

```sh
MSYS_NO_PATHCONV=1 heimdall terraform import azurerm_resource_group.rg /subscriptions/...
```

**`go install …@latest` entrega um binário sem o comando novo.** O proxy do Go
leva alguns minutos para indexar. Use a versão explícita (`@v0.1.0`) ou instale
do checkout.
