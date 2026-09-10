// Package terraform implementa o Terraform Doctor: análise de plano, drift,
// import e migração de state via terraform-exec/terraform-json.
//
// Regra não negociável: nunca editar .tfstate em JSON diretamente — sempre
// via 'terraform state mv', 'import' ou blocos 'moved'.
package terraform
