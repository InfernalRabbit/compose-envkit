# Memory Index

- [Thin engine — compose owns resolution](thin-engine-compose-owns-resolution.md) — cenvkit only assembles the file list; docker compose owns env_file resolution + variable precedence (don't re-engineer it)
- [Verify committed tree, not working tree](verify-committed-tree-during-concurrent-edits.md) — during concurrent edits, test the staged subset (stash -u) before declaring a commit green; a green working-tree test can ship a broken HEAD
