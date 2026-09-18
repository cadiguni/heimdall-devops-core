# Roadmap

Cada item diz **por que** está aqui. Os que vieram de achado real em ambiente de
verdade estão marcados como tal — esses têm prioridade sobre os que vieram de
ideia.

## Entregue

| Comando | Verificado contra |
| --- | --- |
| `terraform plan-review` | fixtures reais de `terraform show -json` |
| `terraform states list` / `show` | container real, 57 states |
| `terraform states rm` / `mv` | state real (preflight); backend local (`--apply`) |
| `terraform import` | state real (preflight); backend local (`--apply`) |
| `pipeline diagnose` | corpus de 11 falhas reais |
| `pipeline variables` | organização real, 63 grupos e 398 variáveis |

## Usar antes de construir

Coisas que as ferramentas já encontraram e que dependem de decisão, não de
código. Valem mais do que qualquer item abaixo.

- **21 variáveis com nome de segredo sem marcação de secreta**, em 13 grupos —
  entre elas PATs do Docker, connection string de Cosmos DB e chaves privadas.
  Marcar como secreta é ação de portal; o `variables show` diz quais são.
- **3 states com `key=` no caminho** (`key=dev/gvpay.tfstate` e companhia). São
  states órfãos com recurso dentro. O caminho é `states show` para ver o que
  têm, depois `import` ou `state mv` para o state certo.
- **7 states suspeitos de estarem vazios**, confirmáveis um a um com
  `states show`. O `prod/redis.tfstate` já foi confirmado vazio.
- **1 state com `$(AliasAssinaturaParaTFState)` no nome** — variável da pipeline
  que não resolveu.

## Próximo

### Perfis de configuração
Todo comando repete `--account --container --key` ou `--org --project`. Um
arquivo no repositório (sem segredo nenhum) mapeando nome → backend resolveria:

```sh
heimdall terraform states list --profile time1-dev
```

É o item mais barato da lista e o que mais reduz atrito no uso diário. Também
diminui a chance do acidente que o projeto inteiro tenta evitar: digitar o
ambiente errado.

### `pipeline connections` — validade de service connection e PAT
**Veio de achado real.** A organização tem seis grupos
`PAT-AZ-SERVICE-CONNECTION-*`, e dentro deles variáveis como
`expires_date_pat` e `docker_expiration_pat`. Ou seja: alguém rastreia validade
de PAT **à mão**, em variável de pipeline.

A API do Azure DevOps sabe a validade de verdade. Um comando que lista service
connections com data de expiração, e avisa das que vencem em N dias, substitui
o controle manual — e ataca uma classe de falha que provavelmente já derrubou
pipeline por lá.

### Versionar e publicar
O README precisa explicar que `go install …@latest` não pega o commit novo,
porque o repositório não tem tag. Uma `v0.1.0` resolve: o time instala por
versão, sem clonar, e `@latest` passa a significar algo.

## Depois

### Migração de recursos entre assinaturas
Mover um resource group entre assinaturas e acertar o tfstate junto. As
complicações estão detalhadas no `CLAUDE.MD`; a pior é o backend morar no RG
que está sendo movido, caso em que o preflight precisa recusar.

### Assinatura de state lock preso
**Bloqueada por falta de dado.** Não há nenhum caso no corpus de 11 falhas. Sem
log real, a mensagem e a extração seriam chute — e é regra do projeto que
assinatura só entra com amostra por trás.

### `pipeline templates`
Está no escopo do `CLAUDE.MD` e nada existe. Precisa de definição antes de
desenho: qual é a dor concreta com os templates compartilhados hoje?

### `heimdall doctor`
Um comando que junta o que os outros fazem: recebe um log de pipeline, roda o
`diagnose` e, para os achados que viram `terraform import`, já executa o
preflight contra o state. Hoje o `diagnose` monta o comando mas não sabe contra
qual backend rodá-lo.

Depende dos perfis de configuração para saber qual backend é qual.

### `cmd/heimdall-server`
O portal HTTP previsto na arquitetura. A lógica de domínio já está em funções
puras sem I/O justamente para isso, mas não há demanda concreta ainda.

## Lacunas conhecidas

Não são planos, são coisas que a documentação afirma e que ainda não foram
provadas:

- Nenhum `--apply` (`import`, `state rm`, `state mv`) rodou contra um backend
  azurerm real. O caminho tem teste de integração com terraform de verdade, mas
  em backend local. Fecha sozinha na primeira operação legítima em dev.
- A leitura da metadata `terraformlockid` só tem teste unitário: o lease usado
  na verificação foi criado por fora do Terraform, então não havia metadata.
  Fecha sozinha no primeiro lock preso de verdade.
- O workflow de CI nunca rodou. A sintaxe do YAML não foi validada localmente,
  só os comandos que ele executa.
