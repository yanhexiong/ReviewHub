export const texEngineValues = [
  "pdflatex",
  "xelatex",
  "lualatex",
  "tectonic",
] as const;

export const texBuildToolValues = ["latexmk", "tectonic", "engine"] as const;

export const texAutoBuildValues = ["off", "on-save", "on-start"] as const;

export const texPdfPreviewValues = [
  "review-hub",
  "latex-workshop",
  "browser",
] as const;

export type TexEngine = (typeof texEngineValues)[number];
export type TexBuildTool = (typeof texBuildToolValues)[number];
export type TexAutoBuild = (typeof texAutoBuildValues)[number];
export type TexPdfPreview = (typeof texPdfPreviewValues)[number];

export type TexLiveConfig = {
  /** Administrator-managed compiler profile; empty means manual settings. */
  profileId: string;
  engine: TexEngine;
  buildTool: TexBuildTool;
  outputDirectory: string;
  autoBuild: TexAutoBuild;
  pdfPreview: TexPdfPreview;
  syncTex: boolean;
  shellEscape: boolean;
  texliveBinPath: string;
  texRootPath: string;
};

export const defaultTexLiveConfig: TexLiveConfig = {
  profileId: "",
  engine: "pdflatex",
  buildTool: "latexmk",
  outputDirectory: "build",
  autoBuild: "off",
  pdfPreview: "review-hub",
  syncTex: true,
  shellEscape: false,
  texliveBinPath: "",
  texRootPath: "",
};

export type TexLiveToolStatus = {
  available: boolean;
  path: string | null;
  version: string | null;
};

export type TexLiveEnvironment = {
  checkedAt: number;
  tools: Record<
    "pdflatex" | "xelatex" | "lualatex" | "latexmk" | "tectonic",
    TexLiveToolStatus
  >;
};
