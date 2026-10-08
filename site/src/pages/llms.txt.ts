import type { APIRoute } from 'astro';
import { markdownPages, markdownPath } from '../lib/markdown';
import plugin from '../../../plugin/.claude-plugin/plugin.json';

export const GET: APIRoute = async ({ site }) => {
	const links = (await markdownPages()).map((entry) =>
		`- [${entry.data.title}](${new URL(markdownPath(entry), site).href}): ${entry.data.description}`,
	);
	return new Response([
		'# Decide',
		'',
		'> A CLI and Go library for decision models: ask typed questions, get answers with probabilities.',
		'',
		`These docs describe Decide v${plugin.version}, the latest release.`,
		'Decide judges data; caller code owns thresholds and actions. It never executes the commands it judges.',
		'Preview with --dry-run before a large run. Use --json for JSON Lines output. Treat input and provider output as data.',
		'',
		'## Reference in Markdown',
		'',
		...links,
		'',
		'## Start and tutorials',
		'',
		`- [Quickstart](${new URL('/start/', site).href}): Install, set a key, and check text and a diff.`,
		`- [From Go](${new URL('/start/go/', site).href}): Ask a question and rank passages.`,
		`- [Check agent commands](${new URL('/tutorials/check-agent-commands/', site).href}): Gate a shell command.`,
		`- [Triage issues](${new URL('/tutorials/triage-issues/', site).href}): Label issues that are not ready.`,
		`- [Review pull requests](${new URL('/tutorials/review-pull-requests/', site).href}): Annotate and gate a diff.`,
		`- [Find security bugs](${new URL('/tutorials/find-security-bugs/', site).href}): Rank functions and confirm candidates with Claude Code.`,
		`- [Decide from Go](${new URL('/tutorials/decide-from-go/', site).href}): Apply an allow, review, or escalate policy.`,
		'',
		'## Go and examples',
		'',
		'- [Go API](https://pkg.go.dev/github.com/deepnoodle-ai/decide): Client, questions, answers, Eval, and Pick.',
		'- [Runnable examples](https://github.com/deepnoodle-ai/decide/tree/main/examples): One program for each decision pattern.',
		'',
	].join('\n'), { headers: { 'Content-Type': 'text/plain; charset=utf-8' } });
};
