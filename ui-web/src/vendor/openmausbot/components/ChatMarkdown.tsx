// Vendored from OpenMausBot `src/components/ChatMarkdown.tsx`.
//
// The render map (`MARKDOWN_COMPONENTS`), the heading/list/table recipes and the code plate
// are upstream's. What could not come along, because each pulls a subsystem this build does
// not have:
//   * Shiki highlighting + mermaid (`components/CodeBlock.tsx`, `MermaidDiagram`): a fenced
//     block draws as plain monospace in the same plate.
//   * Thread refs, mention popovers, GitHub hover cards, link hover cards, local-file links
//     and image preview dialogs: a link is an ordinary external link, an image an <img>.
//   * The block-level streaming parse (`lib/markdown-stream-blocks`) and the render cache:
//     a streaming message parses as a whole, which is what upstream does for finished text.
//   * `remark-math` / `mdast-util-from-markdown` are not dependencies here, so KaTeX and the
//     offset bookkeeping are gone; GFM and soft breaks are kept.
//
// Security: model output never reaches the DOM as raw HTML (no rehype-raw).

import { memo, useMemo, type ComponentProps, type ReactNode } from "react";
import Markdown, { defaultUrlTransform, type Components, type ExtraProps, type Options as MarkdownOptions } from "react-markdown";
import remarkGfm from "remark-gfm";
import remarkBreaks from "remark-breaks";
import { Check, Copy } from "lucide-react";
import { cn } from "../lib/cn";
import { useCopyFeedback } from "../lib/copy-text";

const LINK_CLASS =
  "text-brand-fg underline decoration-brand-fg/25 decoration-1 underline-offset-2 transition-colors hover:decoration-brand-fg/60 break-words";

export const GITHUB_REF_CLASS =
  "inline-flex items-baseline gap-1 text-brand-fg underline decoration-brand-fg/25 decoration-1 underline-offset-2 break-words";

interface BlockProps extends ExtraProps {
  children?: ReactNode;
}

/** Upstream `blockDirection`: a block whose own text is right-to-left says so. */
function blockDirection(node: ExtraProps["node"]): "auto" | undefined {
  const text = flatten(node).trim();
  if (!text) return undefined;
  return /[\u0590-\u08FF\uFB1D-\uFDFF\uFE70-\uFEFF]/.test(text) ? "auto" : undefined;
}

function flatten(node: unknown): string {
  if (!node || typeof node !== "object") return "";
  const record = node as { value?: unknown; children?: unknown };
  if (typeof record.value === "string") return record.value;
  if (Array.isArray(record.children)) return record.children.map(flatten).join(" ");
  return "";
}

/** The plate a fenced block sits in: upstream `CodeBlock`'s frame, without highlighting. */
function MarkdownCode({ children }: ComponentProps<"pre">) {
  const child = Array.isArray(children) ? children[0] : children;
  const className: string = (child as { props?: { className?: string } } | undefined)?.props?.className ?? "";
  const lang = /language-([^\s]+)/.exec(className)?.[1] ?? "";
  const code = flatten(child as unknown).replace(/\n$/, "");
  return <CodePlate code={code} lang={lang} />;
}

export function CodePlate({ code, lang }: { code: string; lang: string }) {
  const { state, copy } = useCopyFeedback(code);
  return (
    <div dir="ltr" data-look="code-block" className="omb-code-block group/code relative my-2.5 overflow-hidden rounded-xl bg-brand-accent/[0.04]">
      <div className="flex items-center justify-between px-3 pt-2 text-[11px] text-brand-fg/45">
        <span className="font-mono lowercase">{lang || "text"}</span>
        <button
          type="button"
          onClick={copy}
          aria-label="复制代码"
          title="复制代码"
          className="inline-flex size-7 items-center justify-center rounded-full text-brand-fg/50 transition-colors hover:bg-brand-accent/[0.06] hover:text-brand-fg focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-accent/40"
        >
          {state === "copied" ? <Check size={15} strokeWidth={1.75} /> : <Copy size={15} strokeWidth={1.75} />}
        </button>
      </div>
      <pre className="overflow-x-auto px-3 pb-3 pt-1.5 font-mono text-[12.5px] leading-[19px] text-brand-fg">
        <code className="whitespace-pre">{code}</code>
      </pre>
    </div>
  );
}

function MarkdownLink({ href, children }: { href?: string; children?: ReactNode }) {
  if (!href) return <span className="break-words">{children}</span>;
  if (!/^https?:\/\//i.test(href)) {
    return (
      <a href={href} target="_blank" rel="noreferrer" dir="auto" className={LINK_CLASS}>
        {children}
      </a>
    );
  }
  return (
    <a href={href} target="_blank" rel="noreferrer" dir="auto" title={href} className={cn(LINK_CLASS, "break-all")}>
      {children}
    </a>
  );
}

export const MARKDOWN_COMPONENTS: Components = {
  pre: MarkdownCode,
  code({ children }: { children?: ReactNode }) {
    return (
      <code dir="ltr" className="omb-code-chip break-words [unicode-bidi:isolate]">
        {children}
      </code>
    );
  },
  a: MarkdownLink,
  table({ children }: BlockProps) {
    return (
      <div className="my-2 overflow-x-auto">
        <table className="w-full border-collapse text-start text-[13px]">{children}</table>
      </div>
    );
  },
  th({ children }: { children?: ReactNode }) {
    return <th className="border-b border-brand-border/60 px-2 py-1.5 text-start font-semibold">{children}</th>;
  },
  td({ children }: { children?: ReactNode }) {
    return <td className="border-b border-brand-border/30 px-2 py-1.5 align-top">{children}</td>;
  },
  p({ node, children }: BlockProps) {
    return <p dir={blockDirection(node)}>{children}</p>;
  },
  ul({ node, children }: BlockProps) {
    return (
      <ul dir={blockDirection(node)} className="list-disc space-y-1 ps-5">
        {children}
      </ul>
    );
  },
  ol({ node, children }: BlockProps) {
    return (
      <ol dir={blockDirection(node)} className="list-decimal space-y-1 ps-5">
        {children}
      </ol>
    );
  },
  h1({ node, children }: BlockProps) {
    return (
      <div dir={blockDirection(node)} className="omb-md-heading mt-3 text-[20px] font-semibold leading-[26px] tracking-[-0.01em]">
        {children}
      </div>
    );
  },
  h2({ node, children }: BlockProps) {
    return (
      <div dir={blockDirection(node)} className="omb-md-heading mt-2 text-[17px] font-semibold leading-6 tracking-[-0.005em]">
        {children}
      </div>
    );
  },
  h3({ node, children }: BlockProps) {
    return (
      <div dir={blockDirection(node)} className="mt-1.5 font-semibold">
        {children}
      </div>
    );
  },
  h4({ node, children }: BlockProps) {
    return (
      <div dir={blockDirection(node)} className="mt-1.5 font-semibold">
        {children}
      </div>
    );
  },
  h5({ node, children }: BlockProps) {
    return (
      <div dir={blockDirection(node)} className="mt-1.5 text-[14px] font-semibold">
        {children}
      </div>
    );
  },
  h6({ node, children }: BlockProps) {
    return (
      <div dir={blockDirection(node)} className="mt-1.5 text-[14px] font-semibold text-brand-fg/60">
        {children}
      </div>
    );
  },
  blockquote({ node, children }: BlockProps) {
    return (
      <blockquote dir={blockDirection(node)} className="border-s-2 border-brand-border ps-3 text-brand-fg/60">
        {children}
      </blockquote>
    );
  },
  hr() {
    return <hr className="border-brand-border/60" />;
  },
};

/** One plugin list for every message, as upstream: GFM, then soft breaks so a plain newline is a line break. */
const PLUGINS: NonNullable<MarkdownOptions["remarkPlugins"]> = [remarkGfm, remarkBreaks];

export interface ChatMarkdownProps {
  text: string;
  /** A reply still arriving: it keeps the same components, and the bubble animates the tail. */
  streaming?: boolean;
  className?: string;
}

export const ChatMarkdown = memo(function ChatMarkdown({ text, streaming = false, className }: ChatMarkdownProps) {
  const components = useMemo(() => MARKDOWN_COMPONENTS, []);
  return (
    <div
      dir="auto"
      data-markdown={streaming ? "streaming" : "finished"}
      className={cn("omb-markdown break-words [overflow-wrap:anywhere]", className)}
    >
      <Markdown remarkPlugins={PLUGINS} components={components} urlTransform={defaultUrlTransform}>
        {text}
      </Markdown>
    </div>
  );
});
