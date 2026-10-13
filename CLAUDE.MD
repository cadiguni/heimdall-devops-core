# heimdall-devops-core

CLI interna em Go para automatizar diagnóstico e correção de problemas de
DevOps: Terraform (drift, import, migração de state), pipelines Azure DevOps,
templates de pipeline compartilhados, Variable Groups e recursos Azure.

## Arquitetura
- `cmd/heimdall/` — entrypoint da CLI (Cobra)
- `internal/core/` — lógica compartilhada entre os módulos, sem I/O direto.
  Hoje: perfis de configuração (parsing recebe `io.Reader`; achar o arquivo é
  responsabilidade do `cmd`).
- `internal/terraform/` — módulo Terraform Doctor. terraform-json para ler
  plano, parser próprio para o arquivo de state (formato v4, que é outro), e
  terraform-exec só no `import --apply`, atrás da interface `Importer`.
- `internal/pipeline/` — módulo Pipeline Doctor
- `internal/azure/` — integrações com serviços Azure: o SDK for Go para Blob
  Storage e a API REST do Azure DevOps. Adapters finos atrás de interfaces
  (`azure.BlobLister`, `azure.VariableGroupLister`), para o domínio ser
  testável sem nuvem.
- Futuro: `cmd/heimdall-server/` reaproveitando os mesmos pacotes `internal/`
  para expor um portal via HTTP.

## Princípios (não negociáveis)
- Nunca editar `.tfstate` em JSON diretamente — sempre `terraform state mv`,
  `import`/`moved` blocks.
- Toda operação potencialmente destrutiva precisa de dry-run por padrão;
  execução real exige flag explícita (`--apply` ou similar).
- Nunca logar secrets em texto puro (Variable Groups, Key Vault refs). Na
  prática isso já barrou duas coisas: o `plan-review` não imprime valor de
  atributo, e não existe caminho de access key para o Azure — só
  `DefaultAzureCredential`, porque chave em flag vaza em histórico de shell,
  em `ps` e em log de pipeline.
- Comando que lê um recurso de ambiente (state, container) precisa dizer no
  cabeçalho contra o que está operando. Os states de dev, hml e prod só se
  distinguem pelo caminho.
- MVP: `terraform plan-review` foi o primeiro módulo e está entregue.
- Preflight bloqueado não imprime o comando equivalente: sugerir seria convidar
  a contornar a verificação.
- Existe um TOCTOU entre o preflight e o `--apply`: o state pode ser travado no
  intervalo. Quem reclama nesse caso é o terraform, com a mensagem dele.
- O heimdall nunca roda `terraform init`. Reconfigurar backend de diretório de
  trabalho alheio pode migrar state; o `init` é mostrado, nunca executado.
- O `terraform state rm` funciona com o recurso fora da configuração e o
  `terraform import` recusa (verificado rodando os dois). Num fluxo que faz rm
  e depois import, isso deixaria o recurso fora do state por causa verificável
  antes — então quem faz os dois verifica a configuração no preflight.
- Em diretório sem backend inicializado o `terraform state pull` sai com código
  0 e saída vazia (verificado). Vazio é caso próprio, não erro de parser.
- Valor de atributo de state não é lido, com uma exceção: `attributes.id`, que
  é o que o `remap` precisa reescrever. A exceção é imposta pelo tipo (a struct
  declara só o campo `id`, e o `encoding/json` descarta o resto), não por
  disciplina de quem escreve o código depois.
- O `remap` não checa existência de recurso no ARM, mas a razão NÃO é falta de
  acesso: o ARM responde 200 para esta conta (verificado em 2026-10-13, e o
  `states coverage` depende disso). A razão é que um check de existência
  precisa valer para quem tem só o acesso mínimo de dados, e ali um 403 é
  indistinguível de "não existe". Quem confirma existência é o `terraform
  import`, pelo provider. Onde o ARM é usado, 401/403 vira erro explícito,
  nunca lista vazia — listagem vazia por falta de permissão leria como
  "a assinatura não tem resource group".

## Convenções
- Go 1.25+ (piso vem do Azure SDK e do terraform-exec), Cobra para subcomandos.
- Testes com fixtures reais de `terraform show -json` em `testdata/`.
- Scripts bash/PowerShell existentes podem ser chamados via `os/exec` como
  ponte temporária até o módulo Go equivalente estar pronto.
- Códigos de saída: 0 sucesso, 1 erro de execução, e de 2 em diante um motivo
  específico de reprovação (2 destruição ou preflight bloqueado, 3 plano
  incompleto, 4 drift). Uma pipeline precisa distinguir "a revisão reprovou"
  de "o heimdall quebrou".
- Saída de texto em português; valores em JSON ficam em identificadores
  estáveis em inglês (`delete`, `replace`, `locked`), traduzidos só na
  renderização.
- Lógica de domínio em funções puras, sem I/O, pensando no
  `cmd/heimdall-server` futuro. Quem faz I/O recebe `io.Writer`/`context`.
- Nenhuma flag de alvo é `MarkFlagRequired`: a validação do cobra roda antes do
  RunE, ou seja antes de o `--profile` ter chance de preencher. Quem valida é o
  `core`, depois da resolução, citando a flag que falta e lembrando do perfil.
- O arquivo de perfis nunca guarda segredo, e é decodificado com
  `KnownFields(true)` para recusar campo desconhecido — inclusive tentativa de
  colocar chave de acesso ali.
- Comando-grupo (que só agrupa subcomandos) precisa de `Args: cobra.NoArgs` e
  o root instala `flagErrorFunc`: o cobra faz parse de flag antes de validar
  argumento, então sem isso um subcomando errado vira erro sobre a flag
  seguinte e esconde a causa.
- A versão vem do `debug.BuildInfo`, não de `-ldflags`: o Go carimba sozinho a
  tag em `go install módulo@versão` e um pseudo-version com commit em
  `go build`. O `-ldflags -X main.version` continua vencendo quando usado.
- Para usar, instalar por versão (`@v0.1.0`); para desenvolver, do checkout
  (`go install ./cmd/heimdall`). O `@latest` pega o que o proxy.golang.org
  indexou, que leva minutos para acompanhar uma tag nova.
- Tudo versionado em LF (ver `.gitattributes`): com `core.autocrlf=true` no
  Windows o `gofmt` acusa todo arquivo como mal formatado.

## Estado atual
- `terraform plan-review` — resumo, operações destrutivas e drift. Gates de
  destruição e de plano incompleto ligados por padrão; o de drift desligado.
- `terraform states list` — lista states de um container Azure, o lock de cada
  um e irregularidades de nome/tamanho. Verificado contra um container real.
  A detecção de lease foi confirmada contra um lease real; a leitura da
  metadata `terraformlockid` só tem teste unitário, porque o blob usado na
  verificação tinha sido travado por fora do Terraform.
- `terraform states show` — resume o que um state rastreia (serial, lineage,
  lista de recursos), sem imprimir valor de atributo. Verificado contra um
  state real.
- `terraform import` — preflight (state existe, destravado, endereço livre) e
  import com dry-run por padrão. O preflight foi verificado contra um state
  real; o `--apply` tem teste de integração com terraform e backend local, mas
  ainda não rodou contra um backend azurerm de verdade.
- `terraform states rm` e `states mv` — cirurgia de state com o mesmo padrão de
  preflight do import, invertido: o endereço precisa existir, e no mv o destino
  precisa estar livre. Lista as instâncias atingidas antes de confirmar, porque
  endereço sem índice pega todo o count/for_each. Verificado contra state real;
  o `--apply` tem teste de integração em backend local.
- `terraform states remap` — troca a assinatura nos ids de um state depois de um
  move de recursos entre assinaturas. Mesmo padrão de preflight, mais o aviso
  sobre o backend. Com `--chdir` (exigido pelo `--apply`)
  também confere o lineage do diretório, se cada alvo tem bloco na configuração
  e se a configuração ainda cita a assinatura antiga. Classifica cada recurso em três grupos (remapeável, id não
  reescrevível, intocado) e para na primeira falha de forma retomável. O plano
  foi verificado contra um move real: o state aponta para uma assinatura que já
  não tem o resource group, e os três ids novos existem na de destino. O
  `--apply` ainda não rodou — precisa do diretório do módulo inicializado.
- `pipeline variables` — lista e detalha Variable Groups do Azure DevOps por
  Entra ID. Valores só com `--show-values`, e valor de secreta nunca, porque a
  API devolve null. Aponta variável cujo nome indica segredo sem estar marcada
  como secreta. Verificado contra uma organização real de 63 grupos e 398
  variáveis.
- `pipeline connections` — lista service connections e classifica se a
  credencial vence. A API do Azure DevOps não expõe data de validade nenhuma;
  o comando responde a pergunta anterior. Esquema desconhecido vira "?", nunca
  "não vence". Verificado contra 61 conexões reais.
- `terraform states coverage` — cruza os resource groups de uma assinatura com
  o que os states de um container rastreiam. Três grupos: com state, sem state
  neste container (candidato a feito à mão), e state que rastreia RG que não
  está mais na assinatura. A evidência é o id dentro do state, não o nome.
  Verificado contra 16 resource groups e 61 states reais.
- `pipeline references` — procura onde uma assinatura ainda é citada: service
  connection (por `data.subscriptionId`), variável de Variable Group e arquivo
  do repositório. Somente leitura, e nenhum valor é impresso. Variável secreta
  vai para "não verificável", porque a API devolve null. Cruza as origens:
  library citando a assinatura sem nenhuma conexão do projeto apontar para ela
  vira nota. Verificado contra a organização real, onde achou 9 grupos.
- `pipeline diagnose` — reconhece falhas em log de pipeline e sugere correção.
  Onze assinaturas derivadas de logs reais. Extrai o backend do log do init
  (a task imprime o -backend-config antes de executar), então o comando de
  import sai completo, apontando ao state da execução. `--fail-on-findings`
  para uso como passo de pipeline.

## Sobre segredos em Variable Groups
- Valor de variável só sai com `--show-values`, nunca por padrão: variável que
  ninguém marcou como secreta guarda segredo o tempo todo, e a saída de um
  comando acaba em log de pipeline.
- Valor de variável secreta nunca sai, nem com a flag. A API do Azure DevOps
  devolve `null` nessas, e o domínio também não copia caso isso mude.
- Autenticação por Entra ID, nunca PAT — mesma razão de não haver access key no
  acesso ao Storage.
- A heurística de "nome indica segredo" erra para os dois lados de propósito, e
  o relatório fala em suspeita. As exclusões (`public`, `expir`) saíram de
  falsos positivos reais, não de especulação.
- Em service connection, `authorization.parameters` não é decodificado: carrega
  material de credencial, e o que não é preciso não deve nem entrar na struct.
- A extração de backend do log pega só account, container e key. Os outros
  parâmetros do -backend-config são credencial e não têm onde ser guardados.
- Assinatura que detecta segredo tem a evidência redigida pelo campo `redact`:
  a evidência é a linha que contém o segredo, e um detector que reimprime o que
  achou não serve para nada. Só nome do parâmetro e tamanho vão para os
  detalhes.
- Token sem o escopo certo faz o Azure DevOps responder HTTP 203 com a página
  de login em vez de 401, e o corpo decodifica em silêncio para lista vazia. O
  adapter reconhece esse status; sem isso, "sem permissão" apareceria como
  "nenhum Variable Group".

## Próximos passos
O planejamento vive em `ROADMAP.md`. Aqui ficam só as decisões de desenho já
tomadas para o que ainda não existe.

## Próximos módulos combinados

### Migração de recursos entre assinaturas
O `states remap` resolve a metade do state: dado que o move já aconteceu, troca
a assinatura nos ids. O que sobrou de decisão de desenho, para quando o move em
si entrar no heimdall:

- O ID do recurso carrega o GUID da assinatura. Depois do move, todo ID no
  state está errado, não só o do RG — cada recurso dentro dele também. É isto
  que o `remap` corrige, por `state rm` + `import`, nunca editando o state.
- O move é assíncrono e pode falhar parcialmente, deixando metade dos recursos
  de cada lado. Por isso o `remap` para na primeira falha e é retomável: rodar
  outra vez continua de onde parou, porque quem já foi remapeado não casa mais
  com o `--from`.
- Nem todo tipo de recurso suporta move entre assinaturas, e o Azure tem uma
  ação de validação (`validateMoveResources`). Ela pertence ao comando que fizer
  o move, não ao `remap`: quando o `remap` roda, o move já aconteceu.
- Armadilha mais séria: se o próprio storage account do backend estiver no RG
  que está sendo movido, o backend some no meio da operação e não há state para
  corrigir depois. O `remap` avisa e não afirma — descobrir em que assinatura
  uma storage account mora é control plane, e quem roda o comando pode ter só
  acesso de dados.
- O que o `remap` não cobre e ficou combinado como fora de escopo: service
  connection (não precisa criar nem editar agora) e a referência à assinatura
  em Variable Group.

## Sobre o catálogo de assinaturas
- Assinatura só entra com log real por trás. Sem amostra, a mensagem e a
  extração seriam chute — e um diagnóstico errado custa mais caro que nenhum.
  É por isso que não existe assinatura de state lock preso: não há caso desses
  no corpus.
- Erro de consequência (`TerraformPlanFailed`, `failed with exit code 1`) não
  vira assinatura: aparece em toda falha e não aponta causa.
- Comando sugerido só é montado com dado extraído do log. Sem o endereço do
  recurso, o achado sai sem comando — melhor nenhum comando que um `import`
  com endereço adivinhado.
- Log cru vai em `logs-raw/` (ignorado pelo git). Fixture em `testdata/` só
  depois de higienizada, e a higienização precisa preservar a semântica do
  caso (ver `internal/pipeline/testdata/README.md`).

## Sobre a referência à assinatura fora do state
- Atualizar Variable Group pela API é `PUT` que substitui o mapa inteiro de
  variáveis (verificado na doc 7.1), e a leitura devolve `null` no valor das
  secretas. GET seguido de PUT apagaria os segredos do grupo, então o heimdall
  não escreve em library.
- O `az pipelines variable-group variable update` **não** tem esse problema:
  altera só a variável indicada e preserva as secretas. Isso é achado do Lucas,
  de script bash dele em produção, não inferência minha — e é o caminho se um
  dia houver escrita.
- `data.subscriptionId` e `data.subscriptionName` da service connection são
  lidos; o resto do `data` não entra na struct. Mesmo critério do
  `authorization.parameters`, ao contrário: ali nada é lido porque tudo é
  credencial, aqui dois campos são lidos porque não são.
- O valor da variável é lido para ser procurado, nunca impresso. É por isso que
  `AllVariableGroupDetails` não tem flag de revelar: quem renderiza referência
  não tem caminho que imprima valor.
- Variável secreta vai para "não verificável", em seção própria. Omitir seria
  dizer que está limpa, e a API não permite saber.
- A varredura do repositório relata arquivo e linha, não a linha. Em tfvars a
  linha pode ser connection string, e o relatório precisa poder ir para log de
  pipeline.

## Sobre distinguir Terraform de feito à mão
- A evidência é o id dentro do state, não o nome do recurso nem o nome do
  Variable Group. O id carrega assinatura e resource group, e é o único sinal
  que não depende de convenção que alguém pode ter quebrado.
- "Sem state" é sempre "sem state NESTE container". Um resource group pode ter
  state em outra conta de armazenamento, e o relatório diz isso em vez de
  concluir. A conclusão depende de saber quais backends o time usa, e isso o
  heimdall não sabe.
- Data source não conta como rastreio: é relido a cada plan e não é gerenciado.
- State ilegível vira nota, nunca silêncio. Tratá-lo como vazio diria que os
  resource groups dele não têm state — a conclusão errada e a mais cara.
- Tag de resource group é o que resta quando não há state. Na organização real,
  `Responsável` e `Setor` estavam preenchidos em todos os sem-state.
