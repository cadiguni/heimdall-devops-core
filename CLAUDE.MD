# heimdall-devops-core

CLI interna em Go para automatizar diagnóstico e correção de problemas de
DevOps: Terraform (drift, import, migração de state), pipelines Azure DevOps,
templates de pipeline compartilhados, Variable Groups e recursos Azure.

## Arquitetura
- `cmd/heimdall/` — entrypoint da CLI (Cobra)
- `internal/core/` — lógica de negócio compartilhada, sem I/O direto
- `internal/terraform/` — módulo Terraform Doctor (usa terraform-json; o
  terraform-exec entra quando houver execução real de terraform, que ainda
  não existe)
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

## Convenções
- Go 1.24+, Cobra para subcomandos.
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
- Tudo versionado em LF (ver `.gitattributes`): com `core.autocrlf=true` no
  Windows o `gofmt` acusa todo arquivo como mal formatado.

## Estado atual
- `terraform plan-review` — funcionando, coberto por testes.
- `terraform states list` — lista states de um container Azure e o lock de
  cada um. Verificado contra um container real. O caminho de lock só tem
  teste unitário: não havia state travado na hora da verificação.
- Pipeline Doctor — não começou. Depende de juntar logs reais de erro de
  pipeline para as assinaturas valerem alguma coisa.