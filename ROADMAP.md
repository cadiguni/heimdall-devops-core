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
| `terraform states remap` | move real: 3 ids novos conferidos no Azure (plano); runner falso (`--apply`) |
| `pipeline diagnose` | corpus de 11 falhas reais |
| `pipeline variables` | organização real, 63 grupos e 398 variáveis |
| `pipeline connections` | organização real, 61 service connections |
| `pipeline references` | organização real: 61 conexões e 67 Variable Groups varridos |
| perfis de configuração | `--profile` verificado contra o container e a organização reais |
| `heimdall version` e tag `v0.1.0` | `go install …@v0.1.0` instala por versão, sem clonar |

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
- **51 das 61 service connections dependem de client secret** (`ServicePrincipal`),
  e só uma usa federação de identidade de carga de trabalho. Cada migração para
  federação elimina uma renovação futura e um motivo de pipeline quebrada.

## Próximo

### Validade real das credenciais, via Microsoft Graph
**A premissa original estava errada.** O roadmap dizia que "a API do Azure
DevOps sabe a validade de verdade". Não sabe: o objeto `ServiceEndpoint` não
tem campo nenhum de expiração — só `authorization.scheme`, `isReady`,
`operationStatus`, tipo e dono. Verificado na referência da API antes de
construir.

O que foi entregue no lugar (`pipeline connections list`) responde a pergunta
anterior: **quais conexões dependem de algo que vence**. Na organização real,
51 de 61 usam `ServicePrincipal`, e só uma usa federação de identidade.

Para a data em si seria preciso o Microsoft Graph: dado o `serviceprincipalid`
da conexão, ler `passwordCredentials[].endDateTime` da app registration. Fica
como item futuro com uma ressalva séria: isso exige permissão de tenant
(`Application.Read.All`), que boa parte dos usuários não tem — e um comando que
só funciona para administrador vale menos que o que já existe.

### Duplicatas e erros de digitação em service connection
**Veio de achado real.** Na listagem de 61 conexões aparecem pares que apontam
para a mesma assinatura e diferem só por grafia:

- `ACR MPN-EDAPPER-NAP(...)` e `ACR-MPN-EDAPPER-NAP(...)`
- `DeviosACR - MPN - IBARTMANN - SB (...)` e `DevopsACR - MPN - IBARTMANN - SB (...)`
  — o primeiro é "Devios", erro de digitação de "Devops"
- `MPN - AMACIEL - (...)` e `MPN - AMACIEL - NAP (...)`

Mesma ideia da detecção de irregularidade nos states: agrupar por GUID de
assinatura no nome e apontar os candidatos a duplicata. Barato, e cada par
desses é uma conexão a menos para renovar quando o segredo vencer.

## Depois

### O move em si, entre assinaturas
O `states remap` já entregou a parte do state: dado que o move aconteceu (hoje
pelo portal de FinOps), ele troca a assinatura nos ids. Falta o move em si — o
`validateMoveResources` do Azure como primeiro passo, e acompanhar a operação
assíncrona até o fim. Só vale construir se o portal de FinOps deixar de ser o
lugar onde o move acontece.

**O que está pendente de verificação, e é mais urgente que construir:** o
`remap --apply`. O plano já foi conferido contra um move real — o
`webappdesenvolvimento-dev-rg`, que saiu da assinatura `92dbecc2` e está na
`51852c54`, com os três ids novos existindo lá. Falta rodar a execução a partir
do diretório do módulo.

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

Agora que os perfis existem, ele tem como saber qual backend é qual: bastaria
um `--profile` no diagnose.

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

## Usar antes de construir — achado do `pipeline references`

**Achado real, 2026-10-13.** Nove Variable Groups do projeto NAP apontam para
uma service connection chamada `MPN - RGRIEBLER - NAP (92dbecc2-...)`, e
nenhuma das 61 service connections do projeto aponta para essa assinatura. Os
grupos: GVhub-Orquestrador dev e prod, GVmodeloExemplo-Frontend-app-dev,
GVPushweb-Api-dev, GVtopicManager-Api-dev, TelemetriaGVcollege-Api dev e hml,
TestProject-Api-dev e WebAppDesenvolvimento-Api-dev.

Não dá para dizer pelo heimdall por que a conexão não está lá — outro projeto,
apagada, ou fora desta listagem. É decisão, não código: ou a conexão volta, ou
os nove grupos passam a apontar para outra. Vale mais que qualquer item abaixo.

## Escrita em library, se um dia fizer sentido
A API de atualização é `PUT` que substitui o mapa de variáveis, e a leitura
devolve `null` nas secretas — GET + PUT apagaria segredo. O caminho viável é o
`az pipelines variable-group variable update`, que altera só a variável
indicada e preserva as secretas (achado do Lucas, de script dele em produção).
Só vale construir se trocar valor em muitos grupos virar rotina; hoje o
`references` aponta e a alteração é manual, de propósito.
