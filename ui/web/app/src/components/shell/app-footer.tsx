export function AppFooter() {
  return (
    <footer className="mt-auto border-t border-border font-mono text-xs text-muted">
      <div className="mx-auto flex min-h-10 w-full max-w-[1280px] items-center gap-2 px-7 max-md:px-5">
        <span>mio operator console</span>
        <span aria-hidden="true" className="text-fg-faint">
          ·
        </span>
        <span>nats · jetstream</span>
        <span aria-hidden="true" className="text-fg-faint">
          ·
        </span>
        <a
          href="https://github.com/crashchat-ai/mio/tree/main/docs"
          target="_blank"
          rel="noreferrer"
          className="text-accent underline-offset-3 hover:text-accent-hover hover:underline"
        >
          Docs
        </a>
      </div>
    </footer>
  );
}
