type Section = { title: string; paragraphs: string[]; important?: boolean };
export type LegalDocument = { intro: string; sections: Section[] };

export const legalDocuments: Record<'terms' | 'privacy', Record<'zh-CN' | 'en', LegalDocument>> = {
  terms: {
    'zh-CN': {
      intro:
        '本协议适用于由 TomyJan（下称“站点维护者”或“我”）维护的“捐赠 TomyJan”网站（donate.tomys.top）。请在登录或付款前阅读本协议，尤其是有关赠与性质、退款及责任的条款。',
      sections: [
        {
          title: '1. 自愿赠与与无偿性质',
          important: true,
          paragraphs: [
            '您向我支付的捐赠是基于真实意愿、无偿作出的个人赠与，并非购买商品、服务、会员权益或投资，也不属于慈善公开募捐。请根据自身经济状况理性决定是否捐赠及捐赠金额。',
            '我不因捐赠而承诺或承担任何特定的服务、咨询、技术支持、内容更新、功能开发、优先回复、交付、回报或其他对价义务。留言、备注或单方提出的要求，不构成我对附加义务的接受；请勿以附带条件的方式捐赠。',
          ],
        },
        {
          title: '2. 付款与退款',
          important: true,
          paragraphs: [
            '付款前请确认金额、付款方式及账号归属。已完成交付的自愿赠与，原则上不提供退款，不支持仅因改变心意或未获得预期回复、服务等理由申请退款。本站不提供自行撤销已支付订单或在线退款的功能。',
            '上述约定不排除法律规定的撤销、返还或其他救济权利。涉及无效或可撤销行为、未成年人未经依法所需的同意、非本人授权付款、重复扣款、支付差错，或其他依法应当返还的情形，应按法律及支付机构的规则处理。请通过本页联系渠道说明订单与争议情况，切勿在公开渠道发送支付凭证或其他敏感信息。',
            '订单是否到账以支付渠道核验结果为准。跳转页面、留言、截图或本站暂时显示的状态，不单独作为最终到账依据；网络延迟可能导致状态更新滞后，请勿反复付款。',
          ],
        },
        {
          title: '3. 登录、账号与适用人群',
          paragraphs: [
            '捐赠需要登录。您可以使用可用的 QQ 或 OIDC 登录方式；授权后，本站会建立账号并保存必要的第三方绑定信息。您应当有权使用相关账号和资金，并保护自己的登录凭证。绑定到同一账号的不同登录方式可访问该账号的捐赠记录。',
            '捐赠原则上应由具有完全民事行为能力的成年人自行作出。未成年人请勿未经监护人依法同意使用本站付款；涉及民事行为能力或授权争议的，依法处理。',
            '为防止滥用、保障账号及支付安全，我可以对违法、冒用身份或其他严重违反本协议的账号采取限制或封禁措施。账号被封禁后，不能登录或捐赠；这不影响您依法主张权利。',
          ],
        },
        {
          title: '4. 捐赠记录与留言',
          paragraphs: [
            '捐赠成功后，昵称、头像、金额、付款日期时间及不超过省级精度的 IP 属地会随捐赠记录公开。留言为选填；填写留言表示您选择在付款成功后随订单公开该内容，请勿填写手机号、地址、证件、支付账号等个人信息。详情见《隐私政策》。',
            '留言应合法、真实，不得侵害他人权益、冒充他人或包含骚扰、广告及其他违法内容。我可以对相关订单或留言进行隐藏、删除或其他必要处理。记录的展示、保留或可见性本身不构成捐赠的对价或承诺。',
          ],
        },
        {
          title: '5. 运行与责任',
          paragraphs: [
            '本站会采取合理措施保障账号、订单与网站安全，但可能因网络、维护或第三方服务故障出现中断或延迟。发生支付异常时，我会在可核验的范围内协助核对，不以本协议免除法律要求承担的责任。',
            '第三方登录、支付渠道及外部链接由相应提供者运营，您使用时还须遵守其规则。本协议不免除我因故意、重大过失或其他依法不得免责情形应承担的责任。',
          ],
        },
        {
          title: '6. 协议更新与争议',
          paragraphs: [
            '本协议适用中华人民共和国大陆地区法律。发生争议时，双方可先协商；协商不成的，可向依法有管辖权的人民法院寻求解决。本协议任何条款不影响法律赋予您的强制性权利。',
            '条款发生实质变更时，本站会更新版本并重新请求登录勾选确认，不以您此前的勾选推定同意新版条款。您可在浏览器清除本机记录，或在登录弹窗取消勾选；这不当然撤销已经完成且依法有效的赠与。',
          ],
        },
      ],
    },
    en: {
      intro:
        'These Terms apply to “Donate TomyJan” (donate.tomys.top), maintained by TomyJan (“I” or the “operator”). Read them before signing in or paying, especially the terms about gifts, refunds and liability.',
      sections: [
        {
          title: '1. A voluntary, gratuitous gift',
          important: true,
          paragraphs: [
            'A donation is a freely chosen personal gift without consideration. It is not a purchase of goods, services, membership or an investment, and is not a public charitable fundraising campaign. Donate only within your means.',
            'A donation does not create any promise or obligation to provide services, consulting, technical support, updates, development, priority replies, deliverables, rewards or other consideration. A message or unilateral request does not mean I accept any attached obligation. Please do not make conditional donations.',
          ],
        },
        {
          title: '2. Payment and refunds',
          important: true,
          paragraphs: [
            'Check your amount, payment method and account before paying. Completed voluntary gifts are generally non-refundable; changing your mind or not receiving an expected response or service is not, by itself, a basis for a refund. The site has no self-service cancellation or refund facility for paid orders.',
            'This does not exclude statutory rights of revocation, restitution or other remedies. Invalid or voidable transactions, payments by minors without legally required consent, unauthorized payments, duplicate charges, payment errors and other circumstances requiring repayment will be handled under applicable law and payment-provider rules. Contact me using the channels on this page with the relevant order and issue; do not post payment evidence or sensitive details publicly.',
            'Payment confirmation depends on verification with the payment provider. Redirects, messages, screenshots or a temporarily displayed status are not conclusive proof of settlement. Updates may be delayed; avoid paying again while a payment is being checked.',
          ],
        },
        {
          title: '3. Sign-in and eligibility',
          paragraphs: [
            'Sign-in is required to donate. Available QQ and OIDC providers can be used; after authorization, the site creates an account and retains necessary account-link information. Use only accounts and funds you are authorized to use and protect your credentials. Sign-in methods linked to one account share its donation history.',
            'Donations should normally be made by adults with full civil capacity. Minors must not make payments without the consent required by law. Disputes about capacity or authorization are handled under applicable law.',
            'Accounts involved in unlawful activity, impersonation or serious abuse may be restricted or banned to protect the site and payments. Banned accounts cannot sign in or donate; statutory rights remain unaffected.',
          ],
        },
        {
          title: '4. Records and messages',
          paragraphs: [
            'After payment, your nickname, avatar, amount, payment date and time, and IP-based location at province level or coarser are publicly displayed. Messages are optional; adding one means choosing to publish it with the paid order. Do not include phone numbers, addresses, identity documents or payment-account information. See the Privacy Policy.',
            'Messages must be lawful and must not infringe rights, impersonate others or contain harassment, advertising or unlawful material. I may hide or remove affected records or messages. Display, retention and visibility of a record are not consideration or a promised benefit of your gift.',
          ],
        },
        {
          title: '5. Operation and responsibility',
          paragraphs: [
            'Reasonable measures protect accounts, orders and site security, but networks, maintenance or third-party failures may cause interruption or delay. I will assist with verifiable payment issues as reasonably possible. These Terms do not exclude liability required by law.',
            'Third-party sign-in, payment services and external links are operated by their respective providers and are also subject to their rules. These Terms do not exclude liability for intent, gross negligence or other circumstances where exclusion is prohibited.',
          ],
        },
        {
          title: '6. Updates and disputes',
          paragraphs: [
            'The laws of mainland China apply. Parties may first seek an amicable resolution; unresolved disputes may be brought before a court with jurisdiction under law. Mandatory statutory rights remain unaffected.',
            'Material changes receive a new version and require a fresh sign-in acknowledgment. A previous selection is not treated as acceptance of a new version. Clear browser storage or uncheck the sign-in box to remove the local acknowledgment; doing so does not itself revoke a completed lawful gift.',
          ],
        },
      ],
    },
  },
  privacy: {
    'zh-CN': {
      intro:
        '本政策说明 TomyJan 作为本站个人信息处理者，在运营 donate.tomys.top 时如何处理您的信息。我们只为网站运行、登录、捐赠记录、支付核验和安全管理处理相关信息，不通过出售个人信息或广告追踪获利。',
      sections: [
        {
          title: '1. 收集的信息与用途',
          paragraphs: [
            '浏览与安全：服务器为响应请求和防止滥用处理连接 IP；运行日志记录请求时间、路径、请求标识及处理结果等必要信息，不记录完整 IP。管理员操作另有审计记录，用于安全管理和排查故障。我们不因此获得您的设备 GPS 精确定位。',
            '登录与绑定：从您选择的 QQ 或 OIDC 提供者获得账号唯一标识、昵称、头像地址等授权信息，并保存本站账号 ID、绑定时间和登录时间。QQ 使用 OpenID 识别账号；配置开启 UnionID 时，同时保存 OpenID 与 UnionID，以 UnionID 维护当前 QQ 应用的账号绑定。QQ 首次登录需要您填写 QQ 号，并通过头像比对辅助核验与授权账号的关联。密码由第三方登录提供者处理，本站不获取您的第三方账号密码。',
            '订单与支付：记录关联用户、金额、支付方式、留言（如填写）、订单及平台流水标识、创建与付款时间、支付状态、核验记录，以及下单 IP 和由该 IP 推断的属地信息，用于创建和核对支付、展示记录、处理异常及保障安全。',
          ],
        },
        {
          title: '2. 公开范围与您的选择',
          important: true,
          paragraphs: [
            '已支付订单默认公开昵称、头像、金额、付款日期时间及不超过省级精度的 IP 属地。填写的留言会随订单公开；请仅填写您愿意公开的内容。游客默认可查看最近 10 条，登录用户可以查看更多，因此公开内容可能被他人读取、转发或截图。',
            '第三方账号标识、QQ 号、完整 IP、平台流水及后台可用的更细属地不会作为公开列表字段展示。订单所有者和授权管理员可按权限查看相应详情。后台属地可包含数据源实际提供的国家、省、市、区、运营商、网络及坐标等；这些是 IP 数据库推断结果，不代表您的实际地址或 GPS 位置。',
            '您可通过下方渠道请求更正、隐藏或删除相关公开信息。管理员也可因安全、违规或本人请求隐藏订单或留言。公开内容被第三方另行保存后，我们可能无法直接删除对方持有的副本。',
          ],
        },
        {
          title: '3. 第三方服务与数据流',
          paragraphs: [
            '登录：QQ 互联（graph.qq.com）或本站配置的 OIDC 提供者接收必要的登录授权请求；QQ 号与头像核验会访问腾讯头像服务（q1.qlogo.cn 等）。绑定时同样需要对应提供者授权。其信息处理受各自政策约束。',
            '支付：本站将处理付款所需的商户订单号、金额、渠道、回调地址、产品说明及支付接口要求的下单 IP 等技术参数发送给配置的彩虹易支付服务；支付宝或微信等实际支付渠道处理付款。本站不要求您提供支付密码，也不在本站保存银行卡号或支付账户密码。',
            '属地：部署可使用本地 IP 数据库，也可使用在线接口。启用在线查询或本地查询的在线回退时，下单 IP 会发送给查询服务；当前支持 ip9.com.cn、ipyou.net、i5p.com 和 whois.pconline.com.cn，按可用性依次尝试。仅发送查询所需 IP，不附带昵称、留言或订单金额。',
            '静态资源：头像、Logo、登录提供者图标和字体可能从第三方地址加载，包括腾讯头像、public-cdn.tomys.top、部署配置的 OIDC 图标地址和 MoeCDN（cdn.amoe.cc）。您的浏览器会直接访问这些资源，服务方可获得访问 IP 等网络请求信息。相关服务的处理与服务器所在地取决于其实际部署，这些访问并非仅在本机发生。',
          ],
        },
        {
          title: '4. Cookie 与本地存储',
          paragraphs: [
            '本站使用登录会话 Cookie（__Host-donate_session）、登录流程校验 Cookie（__Host-donate_flow）和安全校验 Cookie（__Host-donate_csrf）。开发环境名称分别使用 donate_session_dev、donate_flow_dev 和 donate_csrf_dev。生产环境设置 Secure、HttpOnly 与 SameSite=Lax，以减少凭证泄露与跨站请求风险。',
            '会话有效期为 30 天；剩余不超过 20 天时，活跃使用会将其延长至当时起 30 天。退出登录会撤销当前会话并清除对应 Cookie。登录流程校验一般为 10 分钟，安全校验 Cookie 一般为 30 天。浏览器设置或服务端安全操作可能使其提前失效。',
            'localStorage 用于保存语言、外观、条款勾选状态和本提示的收起状态。条款同意与提示收起分别保存，二者互不代替；实质更新版本后重新提示。记录在您清除浏览器数据、取消勾选或新版本替换前保留，不用于广告画像。',
            '您可以在浏览器中清除或阻止 Cookie 和本地存储。阻止必要 Cookie 会影响登录及捐赠；阻止本地存储可能导致偏好与勾选状态无法跨访问保存。底部提示的“知道了”仅收起说明，不代表同意用户协议或授权广告追踪。本站当前未接入广告或访客分析追踪脚本。',
          ],
        },
        {
          title: '5. 保存与安全',
          paragraphs: [
            '账号、订单和绑定信息保存在站点服务器的数据库中；运行与审计日志按部署的保留策略保存。账号信息用于账号持续使用，订单信息用于历史查询与支付核验，我们仅在上述目的、安全排查或法律要求所必要的期间保留信息。达到处理目的、您依法提出删除请求且不存在法定保留事由时，应予删除或匿名化。',
            '本站采用权限控制、会话保护及管理操作审计等措施。只有按职责获得授权的管理员才能访问后台信息；支付凭据等密钥不作为公开接口输出。任何网络系统均无法保证绝对安全，发生信息安全事件时将采取必要补救措施，并依法履行告知义务。',
          ],
        },
        {
          title: '6. 您的权利与联系',
          paragraphs: [
            '您可以查看自己的账号与捐赠记录，并可请求查阅、复制、更正、删除个人信息、撤回相应同意或注销账号。本站暂未提供全部权利的自助入口，请通过下方联系渠道联系 TomyJan；经合理身份核验后依法处理，不要求您公开发送证件、完整 IP 或支付凭证。',
            '撤回同意不影响撤回前合法处理的效力。对履行支付核验、安全或法定义务仍必要的信息，可能依法保留并限制其他处理。我们不会以本政策或技术上没有自助入口为由排除您依法享有的权利。',
          ],
        },
        {
          title: '7. 未成年人和政策更新',
          paragraphs: [
            '本站不面向未成年人提供捐赠服务，不应未经依法所需的监护人同意处理未成年人付款。若监护人发现相关账号、信息或付款，请及时联系，以便核验并依法处理。',
            '处理目的、种类、方式或其他重要内容发生变化时，会更新本页版本并按法律要求重新告知或征得同意。新版不当然溯及既往；本地已保存的旧版勾选不会被视为新版同意。',
          ],
        },
      ],
    },
    en: {
      intro:
        'This Policy explains how TomyJan, the personal information controller for donate.tomys.top, processes information to operate the site, sign-in, donation records, payment verification and security. Personal information is not sold or used to profit from advertising tracking.',
      sections: [
        {
          title: '1. Information and purposes',
          paragraphs: [
            'Browsing and security: connection IPs are processed to serve requests and prevent abuse. Operational logs contain necessary request times, paths, identifiers and results, but not full IP addresses. Administrative actions have separate audit records for security and diagnosis. This does not give us device GPS location.',
            'Sign-in and linking: the selected QQ or OIDC provider supplies authorized account identifiers, nicknames and avatar URLs. We retain the site account ID, linking times and sign-in times. QQ accounts use OpenID; when UnionID mode is configured, both identifiers are retained and UnionID is used for the current QQ application binding. First-time QQ users enter their QQ number; avatar comparison assists verification against the authorized identity. Third-party passwords are handled by the provider, not obtained by this site.',
            'Orders and payment: records contain the associated user, amount, payment method, optional message, order and provider reference IDs, creation and payment times, payment and verification status, request IP and its inferred location. These support payment creation, verification, record display, issue handling and security.',
          ],
        },
        {
          title: '2. Public records and your choices',
          important: true,
          paragraphs: [
            'Paid orders display the nickname, avatar, amount, payment date and time, and IP-based location at province level or coarser. Optional messages are published with paid orders; include only what you intend to make public. Guests see the latest 10 records by default; signed-in users can see more. Others may read, share or capture public content.',
            'Third-party identifiers, QQ numbers, full IP addresses, payment references and finer administrative location fields are not public-list fields. Owners and authorized administrators can view details according to their permissions. Administrative location data may include the country, province, city, district, provider, network or coordinates returned by the source. IP-database estimates are not your actual address or GPS position.',
            'Use the contact channels below to request correction, hiding or removal of public information. Administrators may also hide records or messages for security, policy enforcement or at your request. We may be unable to remove copies independently saved by others.',
          ],
        },
        {
          title: '3. Third-party services',
          paragraphs: [
            'Sign-in: QQ Connect (graph.qq.com) or the configured OIDC provider receives authorization requests. QQ-number and avatar verification accesses Tencent avatar services such as q1.qlogo.cn. Linking also requires provider authorization. Each provider processes information under its own policy.',
            'Payment: the configured Epay service receives the merchant order number, amount, channel, callback URLs, product description and required technical parameters, including the order IP address. Actual channels such as Alipay or WeChat process the payment. This site does not request payment passwords or store bank card numbers or payment-account passwords.',
            'IP location: deployments can use a local database or online services. Online lookup or fallback sends the order IP to the lookup provider. Supported services are ip9.com.cn, ipyou.net, i5p.com and whois.pconline.com.cn, tried according to availability. The lookup does not attach your nickname, message or order amount.',
            'Resources: avatars, the logo, provider icons and fonts can load directly from third-party addresses, including Tencent, public-cdn.tomys.top, the configured OIDC icon URL and MoeCDN (cdn.amoe.cc). Your browser contacts those services, which can receive your IP and network request information. Processing and server locations depend on their deployments; requests do not occur only on your device.',
          ],
        },
        {
          title: '4. Cookies and local storage',
          paragraphs: [
            'The site uses session (__Host-donate_session), sign-in flow (__Host-donate_flow) and security verification (__Host-donate_csrf) cookies. Development names are donate_session_dev, donate_flow_dev and donate_csrf_dev. Production cookies use Secure, HttpOnly and SameSite=Lax protections.',
            'Sessions last 30 days. During active use, a session with 20 days or less remaining is extended to 30 days from that time. Signing out revokes the current session and clears its cookie. Sign-in flow verification normally lasts 10 minutes; the security cookie normally lasts 30 days. Browser settings or server-side security actions can invalidate them earlier.',
            'localStorage holds language, appearance, agreement selection and notice dismissal. Agreement and notice dismissal are separate; material version changes prompt you again. Records remain until cleared, unchecked or superseded and are not used for advertising profiles.',
            'You can clear or block cookies and local storage in your browser. Blocking essential cookies affects sign-in and donations; blocking local storage can prevent preferences and selections from surviving visits. “Got it” dismisses this notice only; it does not accept the Terms or authorize advertising tracking. The site currently includes no advertising or visitor analytics tracking scripts.',
          ],
        },
        {
          title: '5. Retention and security',
          paragraphs: [
            'Account, order and linking information is held in the site server database. Operational and audit logs follow the deployed retention policy. Account information supports continued account use; orders support history and payment verification. Information is retained only as necessary for those purposes, security or legal obligations. When the purpose ends, or a valid deletion request applies without a legal retention ground, information should be deleted or anonymized.',
            'Access controls, session safeguards and administrative auditing protect information. Only authorized administrators acting within their duties can access administration. Payment credentials and other secrets are not returned by public APIs. No system guarantees absolute security; incidents will receive remedial action and legally required notifications.',
          ],
        },
        {
          title: '6. Your rights and contact',
          paragraphs: [
            'You can view your account and donation history and request access, copies, correction, deletion, withdrawal of applicable consent or account closure. Not all rights have self-service controls yet; contact TomyJan below. Requests will be handled under applicable law after reasonable identity verification. Do not publicly send identity documents, full IP addresses or payment evidence.',
            'Withdrawal does not affect lawful processing before withdrawal. Information still necessary for payment verification, security or legal duties may be retained with further use restricted as required. Neither this Policy nor missing self-service controls removes statutory rights.',
          ],
        },
        {
          title: '7. Minors and updates',
          paragraphs: [
            'The donation service is not intended for minors. Payments must not be processed without legally required guardian consent. Guardians who discover relevant accounts, information or payments should contact us for verification and handling under law.',
            'Material changes to purposes, information types, processing methods or other important terms will update this version and trigger legally required notice or consent. New terms are not automatically retroactive. An old local agreement is not treated as acceptance of a new version.',
          ],
        },
      ],
    },
  },
};
