export interface TelegramUser {
  id: number;
  first_name: string;
  last_name?: string;
  username?: string;
}

export interface TelegramChat {
  id: number;
  type: 'private' | 'group' | 'supergroup' | 'channel';
}

export interface TelegramUpdate {
  message?: {
    chat: TelegramChat;
    from?: TelegramUser;
    text?: string;
  };
  callbackQuery?: {
    id: string;
    from: TelegramUser;
    data?: string;
    message?: {
      messageId: number;
      chat: TelegramChat;
    };
  };
}
