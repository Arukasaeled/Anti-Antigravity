# Issue tracker: GitHub

仓库：arukas0623-ai/Anti-Antigravity。
工程 skills 使用 gh CLI 操作 GitHub Issues。
运行时从 git remote 推断仓库；遵循当前会话的操作授权。

- 创建：gh issue create --title "..." --body-file <文件>
- 读取：gh issue view <编号> --comments
- 列表：gh issue list --state open --json number,title,body,labels
- 评论：gh issue comment <编号> --body-file <文件>
- 标签：gh issue edit <编号> --add-label "..." / --remove-label "..."
- 关闭：gh issue close <编号> --comment "..."

多行正文写入临时文件，以 --body-file 传入。
PRs as a request surface: no.
“publish to the issue tracker”指创建 GitHub issue；
“fetch the relevant ticket”指读取 issue 及评论。
