# Vision

## The Problem

I have tens of bug fixes, tweaks, and changes I want to make to my software every day. The planning is easy — I can describe what I want clearly and quickly. The bottleneck is implementation. I can endlessly populate a backlog, but the work sits there because executing each item still requires me to sit down, set up context, and babysit the process.

Meanwhile, I have a powerful home server that sits idle most of the day.

## What Hive Does

Hive is a personal agent orchestrator. It turns a backlog of described work into completed pull requests, running on my server, without me in the loop.

The workflow:

1. I drag an issue from Planning to Ready on my GitHub Projects board
2. Hive picks it up, creates an isolated workspace, and launches an agent
3. The agent implements the change, tests it, and commits
4. Hive pushes the branch and opens a PR
5. I review the PR when I'm ready

That's it. My role is intent and review. The agents do the implementation.

## The End State

AIVA — my AI assistant — is the front end. I describe what I want in conversation. AIVA files the issue, moves it to Ready, and Hive takes it from there. I come back to pull requests.

Any stray thought of "this could be better" becomes a functioning change without me touching a keyboard for implementation. The gap between intent and artifact is minutes, not hours.

## What Hive Is Not

- **Not Claude Code.** Hive doesn't implement anything. It orchestrates: workspaces, isolation, dispatch, publishing. Claude Code (or another agent) does the actual work inside the sandbox.
- **Not a CI system.** CI validates code that's already written. Hive produces the code.
- **Not a product.** This is a personal tool for one person. It will never be packaged, supported, or accept contributions. Others can learn from it, but it is not for them.
- **Not infrastructure.** Hive is a tool that runs on infrastructure. It should take days to build, not weeks. If it's taking longer, I'm over-engineering it.

## How I Stay Honest

- If I'm spending more time on Hive than on the work Hive is supposed to do, something is wrong.
- A shell prototype already works. The Go rewrite earns its keep by being correct, composable, and maintainable — not by being ambitious.
- Every decision should make the next dispatch faster and more reliable, not more configurable.
- When I feel the urge to add features before using the basic pipeline daily, stop.
