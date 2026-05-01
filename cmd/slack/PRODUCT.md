# Product

## Register

product

## Users

Developers configuring cula's Slack bot example on their own machine. They are usually in a terminal, wiring Slack Socket Mode credentials to a local agent runtime and choosing a working directory before starting the bot.

## Product Purpose

The Slack example shows how cula can expose local agent runtimes through Slack. Its setup UI should make the required token, runtime, model, and working-directory choices clear enough that a developer can get from environment variables to a running bot without reading code.

## Brand Personality

Local-first, pragmatic, technically calm. The interface should feel like a precise developer tool, not a marketing demo.

## Anti-references

Avoid generic SaaS gloss, noisy rainbow terminal themes, decorative color with no state meaning, and clever custom controls that obscure standard terminal navigation.

## Design Principles

- Make setup state obvious at a glance.
- Use color as meaning: selection, success, warning, error, and help.
- Keep terminal output compact and predictable.
- Prefer developer-tool familiarity over visual novelty.

## Accessibility & Inclusion

Target readable contrast in common light and dark terminals. Do not rely on color alone: selected rows also use a cursor marker, errors keep explicit labels, and unavailable runtimes include text.
