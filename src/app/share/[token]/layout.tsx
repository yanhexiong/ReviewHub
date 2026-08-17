const staticShareToken = "__review_hub_share__";

// Shared-review tokens are resolved client-side from the browser location.
export function generateStaticParams() {
  return [{ token: staticShareToken }];
}

export default function ShareLayout({
  children,
}: Readonly<{ children: React.ReactNode }>) {
  return children;
}
