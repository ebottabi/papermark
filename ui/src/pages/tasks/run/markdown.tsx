import ReactMarkdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import { cn } from "@/lib/utils";

// Hand-rolled prose styling (no typography plugin in this project). Prose uses
// Hanken Grotesk; code switches to IBM Plex Mono per the design identity.
const components: Components = {
  p: ({ children }) => <p className="my-2 first:mt-0 last:mb-0 leading-relaxed">{children}</p>,
  h1: ({ children }) => <h1 className="mb-2 mt-4 text-h-3 font-semibold text-ink first:mt-0">{children}</h1>,
  h2: ({ children }) => <h2 className="mb-2 mt-4 text-body-1 font-semibold text-ink first:mt-0">{children}</h2>,
  h3: ({ children }) => <h3 className="mb-1.5 mt-3 text-body-2 font-semibold text-ink first:mt-0">{children}</h3>,
  ul: ({ children }) => <ul className="my-2 list-disc space-y-1 pl-5">{children}</ul>,
  ol: ({ children }) => <ol className="my-2 list-decimal space-y-1 pl-5">{children}</ol>,
  li: ({ children }) => <li className="leading-relaxed">{children}</li>,
  a: ({ href, children }) => (
    <a href={href} target="_blank" rel="noreferrer" className="font-medium text-accent-ink underline underline-offset-2">
      {children}
    </a>
  ),
  strong: ({ children }) => <strong className="font-semibold text-ink">{children}</strong>,
  em: ({ children }) => <em className="italic">{children}</em>,
  blockquote: ({ children }) => (
    <blockquote className="my-2 border-l-2 border-gray-300 pl-3 text-ink-muted">{children}</blockquote>
  ),
  hr: () => <hr className="my-3 border-gray-200" />,
  code: ({ children, className }) =>
    // react-markdown v9 wraps fenced blocks in <pre>; inline code lands here bare.
    className ? (
      <code className={cn("font-mono text-body-3", className)}>{children}</code>
    ) : (
      <code className="rounded bg-gray-100 px-1 py-0.5 font-mono text-[0.92em] text-ink">{children}</code>
    ),
  pre: ({ children }) => (
    <pre className="scrollbar-thin my-2 overflow-auto rounded-lg border border-gray-200 bg-gray-50 p-3 font-mono text-body-3 text-ink-muted">
      {children}
    </pre>
  ),
  table: ({ children }) => (
    <div className="scrollbar-thin my-2 overflow-x-auto">
      <table className="w-full border-collapse text-body-3">{children}</table>
    </div>
  ),
  th: ({ children }) => <th className="border border-gray-200 bg-gray-50 px-2 py-1 text-left font-semibold">{children}</th>,
  td: ({ children }) => <td className="border border-gray-200 px-2 py-1">{children}</td>,
};

export const Markdown = ({ children, className }: { children: string; className?: string }) => (
  <div className={cn("text-body-2 text-ink", className)}>
    <ReactMarkdown remarkPlugins={[remarkGfm]} components={components}>
      {children}
    </ReactMarkdown>
  </div>
);
