# Pull requests and reviews

tuohi has two forges. Internal work goes through the internal Forgejo
(`origin`). The public GitHub repository (`github`) is the mirror, and it is
where the macOS and Windows engines are tested. The two meet at `main`, which
`just sync-github` keeps equal on both.

## Internal work, on Forgejo

1. Branch from `main`, commit, and push the branch to `origin`.
2. Open the pull request with `tea pulls create`, or through the API.
3. `.forgejo/workflows/ci.yml` is the gate. It runs the steps of `just ci`.
4. Request a model review when the pull request is ready. See
   [terva-review](#terva-review).
5. Post a disposition for each finding, and record it in the ticket, while
   the pull request is still open.
6. Merge on Forgejo, then run `just sync-github --yes` to fast-forward
   GitHub `main`. The GitHub workflow then runs the engines on macOS,
   Windows, and Linux. A failure there is a regression to fix on Forgejo.

## Pull requests on GitHub

Somebody outside terva-sh opens their pull request on GitHub, where
`.github/workflows/ci.yml` runs. `terva-review` does not run there. After a
merge on GitHub, run `just sync-github --yes` to fast-forward Forgejo `main`.

## Keeping main equal

`just sync-github` fetches both `main` branches and fast-forwards the one
that is behind. Without `--yes` it prints the commits it would push and stops.
It never force-pushes. If both forges merged something since the last sync,
the recipe stops with both heads. Then:

1. Branch from `origin/main` and merge `github/main` into the branch.
2. Land the branch through a Forgejo pull request.
3. Run `just sync-github --yes`.

## terva-review

`.forgejo/workflows/terva-review.yml` runs the reviewer from
`terva-sh/terva-action-code-review` on internal pull requests. It is advisory
and does not replace CI. Request a review when a pull request is ready, and
again after substantive fixes.

```sh
tea actions workflows dispatch terva-review.yml --ref main \
  --input pr=PR_NUMBER --input request-id=ready-review
```

The dispatch may answer "unexpected end of JSON input" even when the run was
created. Check the Actions page before retrying.

A maintainer can also comment `/terva review HEAD_SHA BASE_SHA` on the pull
request, with full lowercase SHAs.

### Carry

After commits that touch only `.tickets/`, dispatch with `--input carry=true`.
That copies the last review's result to the new head without a model run, or
refuses and names the path that needs a real review.

A passing review is evidence, not permission to merge.
