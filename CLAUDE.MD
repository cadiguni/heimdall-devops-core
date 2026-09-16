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
- `internal/azure/` — integrações Azure SDK for Go, como adapters finos atrás
  de interfaces (`azure.BlobLister`), para o domínio ser testável sem nuvem
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
- O heimdall nunca roda `terraform init`. Reconfigurar backend de diretório de
  trabalho alheio pode migrar state; o `init` é mostrado, nunca executado.

## Convenções
- Go 1.25+ (piso vem do Azure SDK e do terraform-exec), Cobra para subcomandos.
- Testes com fixtures reais de `terraform show -json` em `testdata/`.
- Scripts bash/PowerShell existentes podem ser chamados via `os/exec` como
  ponte temporária até o módulo Go equivalente estar pronto.
- Códigos de saída: 0 sucesso, 1 erro de execução, e de 2 em diante um motivo
  específico de reprovação (2 destruição, 3 plano incompleto). Uma pipeline
  precisa distinguir "a revisão reprovou" de "o heimdall quebrou".
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
- `terraform plan-review` — funcionando, coberto por testes.
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
- `pipeline diagnose` — reconhece falhas em log de pipeline e sugere correção.
  Dez assinaturas, todas derivadas de um corpus de 11 falhas reais.

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