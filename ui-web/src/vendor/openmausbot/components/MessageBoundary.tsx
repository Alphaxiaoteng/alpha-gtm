// Vendored from OpenMausBot `src/components/MessageBoundary.tsx`.
//
// Adaptation: the fallback bubble used the interface-mode hook to pick Simple vs Advanced
// sizing; the vendored copy takes `simple` as a prop so the boundary stays hook-free.

import { Component, type ReactNode } from "react";

import { plainBotBubble } from "./chat-bubble";

function FallbackBubble({ text, simple }: { text: string; simple: boolean }) {
  return (
    <div data-chat-bubble={simple ? "simple" : ""} className={plainBotBubble(simple)}>
      {text}
    </div>
  );
}

export class MessageBoundary extends Component<
  { children: ReactNode; fallbackText: string; simple?: boolean },
  { failed: boolean }
> {
  state = { failed: false };
  static getDerivedStateFromError() {
    return { failed: true };
  }
  render() {
    if (this.state.failed) {
      return <FallbackBubble text={this.props.fallbackText} simple={this.props.simple ?? true} />;
    }
    return this.props.children;
  }
}
