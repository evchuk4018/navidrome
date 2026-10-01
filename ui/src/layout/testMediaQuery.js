export const installMatchMedia = (initialWidth = 1024) => {
  let width = initialWidth
  const queries = new Map()
  const matches = (query) => {
    const max = query.match(/max-width:\s*([\d.]+)px/)
    const min = query.match(/min-width:\s*([\d.]+)px/)
    return (
      (!max || width <= Number(max[1])) && (!min || width >= Number(min[1]))
    )
  }
  vi.stubGlobal(
    'matchMedia',
    vi.fn((query) => {
      if (!queries.has(query)) {
        const listeners = new Set()
        queries.set(query, {
          media: query,
          get matches() {
            return matches(query)
          },
          addListener: (listener) => listeners.add(listener),
          removeListener: (listener) => listeners.delete(listener),
          listeners,
        })
      }
      return queries.get(query)
    }),
  )
  return (nextWidth) => {
    const previous = new Map(
      [...queries].map(([query, media]) => [query, media.matches]),
    )
    width = nextWidth
    queries.forEach((media, query) => {
      if (previous.get(query) !== media.matches) {
        media.listeners.forEach((listener) => listener(media))
      }
    })
  }
}

// jsdom does not evaluate media queries. Inspect the stylesheet emitted by JSS
// to verify responsive and reduced-motion rules, including nested media blocks.
export const stylesheetRules = (rules, media = []) =>
  Array.from(rules).flatMap((rule) =>
    rule.cssRules
      ? stylesheetRules(rule.cssRules, [...media, rule.conditionText])
      : [{ rule, media }],
  )

export const emittedRules = () =>
  Array.from(document.styleSheets).flatMap((sheet) =>
    stylesheetRules(sheet.cssRules),
  )
