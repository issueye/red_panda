import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { classNames } from '../../lib/format.js';
import { openInExplorer } from '../../lib/desktopShell.js';
import { resolveWorkspaceFileHref } from '../../lib/workspaceLinks.js';

/**
 * Safe markdown renderer for chat messages.
 * Does not enable raw HTML (no rehype-raw).
 * @param {{ children?: string, className?: string, workspaceRoot?: string }} props
 */
export function Markdown({ children, className, workspaceRoot = '' }) {
  const source = typeof children === 'string' ? children : '';
  if (!source.trim()) {
    return null;
  }

  return (
    <div className={classNames('markdown-body', className)}>
      <ReactMarkdown
        components={{
          a: ({ href, children: linkChildren }) => {
            const workspacePath = resolveWorkspaceFileHref(href, workspaceRoot);
            if (workspacePath) {
              return (
                <a
                  data-workspace-file-link="true"
                  href={href}
                  onClick={(event) => {
                    event.preventDefault();
                    openInExplorer(workspacePath);
                  }}
                  title="在资源管理器中定位文件"
                >
                  {linkChildren}
                </a>
              );
            }
            return (
              <a href={href} rel="noreferrer noopener" target="_blank">
                {linkChildren}
              </a>
            );
          },
          pre: ({ children: preChildren }) => <pre className="markdown-pre">{preChildren}</pre>,
          code: ({ className: codeClass, children: codeChildren, ...props }) => {
            const isBlock = Boolean(codeClass) || String(codeChildren).includes('\n');
            if (isBlock) {
              return (
                <code className={classNames('markdown-code-block', codeClass)} {...props}>
                  {codeChildren}
                </code>
              );
            }
            return (
              <code className="markdown-code-inline" {...props}>
                {codeChildren}
              </code>
            );
          },
        }}
        remarkPlugins={[remarkGfm]}
      >
        {source}
      </ReactMarkdown>
    </div>
  );
}
