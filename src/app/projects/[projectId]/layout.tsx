const staticProjectID = "__review_hub_project__";

// AppImage serves a single pre-rendered project shell for all project IDs.
// The client reads the actual ID from the location after hydration.
export function generateStaticParams() {
  return [{ projectId: staticProjectID }];
}

export default function ProjectLayout({
  children,
}: Readonly<{ children: React.ReactNode }>) {
  return children;
}
