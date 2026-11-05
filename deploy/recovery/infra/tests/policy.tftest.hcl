mock_provider "aws" {
  override_during = plan
  mock_data "aws_caller_identity" {
    defaults = { account_id = "123456789012" }
  }
  mock_resource "aws_lambda_function" {
    defaults = { qualified_arn = "arn:aws:lambda:eu-west-2:123456789012:function:fixture:1" }
  }
  mock_resource "aws_cloudwatch_log_group" {
    defaults = { arn = "arn:aws:logs:eu-west-2:123456789012:log-group:fixture" }
  }
  mock_resource "aws_dynamodb_table" {
    defaults = { arn = "arn:aws:dynamodb:eu-west-2:123456789012:table/fixture" }
  }
  mock_resource "aws_kms_key" {
    defaults = { arn = "arn:aws:kms:eu-west-2:123456789012:key/fixture" }
  }

}

variables {
  authority_image                 = "123456789012.dkr.ecr.eu-west-2.amazonaws.com/vellum@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
  github_oidc_provider_arn        = "arn:aws:iam::123456789012:oidc-provider/token.actions.githubusercontent.com"
  roles_anywhere_profile_arn      = "arn:aws:rolesanywhere:eu-west-2:123456789012:profile/fixture"
  roles_anywhere_trust_anchor_arn = "arn:aws:rolesanywhere:eu-west-2:123456789012:trust-anchor/fixture"
  relay_certificate_common_name   = "vellum-production-relay"
  authority_configuration = {
    github_secret  = "arn:aws:secretsmanager:eu-west-2:123456789012:secret:fixture"
    receipt_bucket = "fixture-receipts"
    staging = {
      document = "arn:aws:ssm:eu-west-2:123456789012:document/VellumDeploy-staging"
    }
    production = {
      document = "arn:aws:ssm:eu-west-2:123456789012:document/VellumDeploy-production"
      instance = "i-1234567890abcdef0"
    }
  }
}

run "separate_signing_dispatch_and_invocation" {
  command = plan

  assert {
    condition = alltrue([
      !strcontains(aws_iam_role_policy.runner.policy, "ssm:"),
      !strcontains(aws_iam_role_policy.relay.policy, "ssm:SendCommand"),
      !strcontains(aws_iam_role_policy.runner.policy, "kms:Sign"),
      !strcontains(aws_iam_role_policy.relay.policy, "kms:Sign"),
      !strcontains(aws_iam_role_policy.authority["dispatch"].policy, "kms:Sign"),
      !strcontains(aws_iam_role_policy.authority["host"].policy, "ssm:SendCommand"),
      !strcontains(aws_iam_role_policy.authority["host"].policy, "production/runs/")
    ])
    error_message = "GitHub, relay, dispatch and signing identities must remain separate."
  }
}
