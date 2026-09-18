# heimdall-devops-core

CLI interna em Go para automatizar diagnóstico e correção de problemas de
DevOps: Terraform (drift, import, migração de state), pipelines Azure DevOps,
templates de pipeline compartilhados, Variable Groups e recursos Azure.

## Arquitetura
- `cmd/heimdall/` — entrypoint da CLI (Cobra)
- `internal/core/` — lógica de negócio compartilhada, sem I/O direto
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
- Comando-grupo (que só agrupa subcomandos) precisa de `Args: cobra.NoArgs` e
  o root instala `flagErrorFunc`: o cobra faz parse de flag antes de validar
  argumento, então sem isso um subcomando errado vira erro sobre a flag
  seguinte e esconde a causa.
- Instalar sempre do checkout (`go install ./cmd/heimdall`). O `@latest` pega o
  que o proxy.golang.org indexou, que fica atrás do `main` depois de um push.
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
- `pipeline variables` — lista e detalha Variable Groups do Azure DevOps por
  Entra ID. Valores só com `--show-values`, e valor de secreta nunca, porque a
  API devolve null. Aponta variável cujo nome indica segredo sem estar marcada
  como secreta. Verificado contra uma organização real de 63 grupos e 398
  variáveis.
- `pipeline diagnose` — reconhece falhas em log de pipeline e sugere correção.
  Dez assinaturas, todas derivadas de um corpus de 11 falhas reais.

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
- Token sem o escopo certo faz o Azure DevOps responder HTTP 203 com a página
  de login em vez de 401, e o corpo decodifica em silêncio para lista vazia. O
  adapter reconhece esse status; sem isso, "sem permissão" apareceria como
  "nenhum Variable Group".

## Próximos módulos combinados

### Migração de recursos entre assinaturas
Mover um resource group de uma assinatura para outra e acertar o tfstate na
mesma operação. As complicações conhecidas, para desenhar em cima delas:

- O ID do recurso carrega o GUID da assinatura. Depois do move, todo ID no
  state está errado, não só o do RG — cada recurso dentro dele também.
- Corrigir isso é `state rm` + `import` por recurso (nunca editar o state, por
  princípio), e o import precisa do ID novo, que só existe depois do move.
- Nem todo tipo de recurso suporta move entre assinaturas, e o Azure tem uma
  ação de validação (`validateMoveResources`) que deveria ser o primeiro passo
  do preflight.
- O move é assíncrono e pode falhar parcialmente, deixando metade dos recursos
  de cada lado — o comando precisa ser retomável, não atômico.
- Armadilha mais séria: se o próprio storage account do backend estiver no RG
  que está sendo movido, o backend some no meio da operação e não há state para
  corrigir depois. O preflight precisa detectar esse caso e recusar.

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