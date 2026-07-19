export const mcpClientConfig = (endpointUrl: string, token = '<API_KEY>') =>
  JSON.stringify(
    {
      mcpServers: {
        sigmo: {
          url: endpointUrl,
          headers: { Authorization: `Bearer ${token}` },
        },
      },
    },
    null,
    2,
  )
