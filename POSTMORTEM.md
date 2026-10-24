# Sigvane postmortem

## What it was

Sigvane was a hosted webhook inbox for GitHub. You created an inbox, pointed a repository or organization webhook at its
URL, and Sigvane verified each delivery's signature and stored it. Your own code then pulled the events through the REST
API or the CLI, allowing a worker running locally, in CI, or behind a firewall to react without exposing a public
endpoint.

It had four parts: a Kotlin and Spring Boot backend, an Angular web app, this Go CLI, and a docs site built with Astro.

## Design choices

- **Write-blind.** Sigvane never connected to repositories or held credentials. It only saw what GitHub sent.
- **Verify on receive.** Each inbox had its own secret. Deliveries with bad signatures were dropped before storage.
- **Pull, not push.** Workers fetched events when ready, so nothing needed inbound access to private networks.

## Timeline

I built the first prototype in 2025 and ran it privately for a while before publishing it in spring 2026. After May, the
work was mostly dependency updates, and I shut the service down on 24 October 2026.

## Who used it, and why I shut it down

I built it for myself, and a couple of friends used it too. It worked well for us, but I also wanted to see whether
other people needed it, and mostly they didn't. Polling the GitHub API, running the work in GitHub Actions, or opening a
tunnel already covers most of these cases without introducing another hosted service. Since I also no longer had a
project that needed it, I shut it down.

## What I learned

Even a small personal tool carries a real ongoing cost once you run it as a public service. Keeping other people's data
safe, staying online, and keeping up with dependency updates, security patches, and server bills takes time long after
the initial build is done. That work pays off when many people rely on the service, but with only a few users, my time
was better spent elsewhere. Shutting it down was the right call.

## What happened to the data

All accounts, inboxes, and stored webhook events were deleted on 24 October 2026, including backups. The code stays on
GitHub for reference: the CLI in this repo, and the backend in a private one.