# Code Review Workflow

This workflow reviews the latest open pull request for the configured GitCode project.

1. Read the `[Code Review Config]` block from the task input.
2. Locate the latest open pull request for `gitcode_project`. If the GitCode API or CLI is unavailable, ask the operator for the PR URL or IID instead of guessing.
3. Fetch the pull request branch into the task workspace and compare it against its target branch.
4. Run Codex's built-in code review mode over the pull request diff. Focus on correctness, regressions, security, concurrency, data loss, and missing tests.
5. Produce review findings with file path, line, severity, problem, and concrete suggested wording.
6. Rewrite the review text with `humanizer:humanizer` so it reads like direct engineering feedback, not AI-generated prose.
7. Send the draft comments to the operator for approval through the existing task approval path.
8. Publish only approved comments through GitCode. Discard rejected comments.
9. If the operator requests wording changes, revise the comment text and submit the revised version to GitCode after approval.
10. Finish with a short summary listing reviewed PR, published comments, discarded comments, and any risks that could not be verified.
