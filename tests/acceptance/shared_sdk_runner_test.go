package acceptance

import "context"

func runSharedSDKFixture(ctx context.Context, root, uv, base, audio, format string, fixture clientCodecFixture, run func(context.Context, string, string, ...string) ([]byte, error)) clientCaseResult {
	stdout, commandErr := run(ctx, root, uv, "run", "--locked", "python", "tests/acceptance/openai_client.py", "--base-url", base, "--audio", audio, "--response-format", format, "--timeout-seconds", "120")
	result, validationErr := readClientFixtureCaseResult(stdout, "openai", format, fixture)
	result.Name, result.ExpectedVersion = "official-openai-"+format+"/"+fixture.Name, "2.11.0"
	result.Succeeded = commandErr == nil && validationErr == nil
	return result
}
