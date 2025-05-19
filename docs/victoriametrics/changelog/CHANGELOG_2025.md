---
weight: 2
title: Year 2025
search:
  weight: 0.1
menu:
  docs:
    identifier: vm-changelog-2025
    parent: vm-changelog
    weight: 2
tags:
  - metrics
aliases:
  - /CHANGELOG_2025.html
  - /changelog_2025
  - /changelog/changelog_2025/index.html
  - /changelog/changelog_2025/
---
{{% content "CHANGELOG.md" %}}

* FEATURE: streaming aggregation now supports `dedup_use_insert_timestamp` option
  and corresponding `-streamAggr.dedupUseInsertTimestamp` and
  `-remoteWrite.streamAggr.dedupUseInsertTimestamp` flags for preferring samples
  with the highest insert timestamp during deduplication.
