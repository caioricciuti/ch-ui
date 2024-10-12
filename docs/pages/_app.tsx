import type { AppProps } from "next/app";
import type { ReactElement } from "react";
import "../style.css";

export default function App({ Component, pageProps }: AppProps): ReactElement {
  "use client";
  return <Component {...pageProps} />;
}
