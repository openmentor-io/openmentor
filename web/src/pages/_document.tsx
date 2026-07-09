import Document, { Html, Head, Main, NextScript } from 'next/document'

export default class MyDocument extends Document {
  render(): JSX.Element {
    return (
      <Html lang="en">
        <Head>
          {/* Favicons from the Fresh Signal brand pack (../brand in the monorepo) */}
          <link rel="icon" href="/favicon.ico" sizes="any" />
          <link
            rel="icon"
            type="image/png"
            sizes="32x32"
            href="/brand/logo/png/favicon/favicon-32.png"
          />
          <link
            rel="icon"
            type="image/png"
            sizes="16x16"
            href="/brand/logo/png/favicon/favicon-16.png"
          />
          <link rel="apple-touch-icon" href="/brand/logo/png/icon-app/icon-app-180.png" />
          <link rel="manifest" href="/site.webmanifest" />
          <meta name="theme-color" content="#132A52" />
        </Head>
        <body>
          <Main />
          <NextScript />
        </body>
      </Html>
    )
  }
}
