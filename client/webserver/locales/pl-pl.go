package locales

import "decred.org/dcrdex/client/intl"

var PlPL = map[string]*intl.Translation{
	"Language":                       {T: "pl-PL"},
	"Markets":                        {T: "Rynki"},
	"Wallets":                        {T: "Portfele"},
	"Notifications":                  {T: "Powiadomienia"},
	"Recent Activity":                {T: "Ostatnia aktywność"},
	"Sign Out":                       {T: "Wyloguj się"},
	"Order History":                  {T: "Historia zleceń"},
	"load from file":                 {T: "wczytaj z pliku"},
	"loaded from file":               {T: "wczytano z pliku"},
	"defaults":                       {T: "domyślne"},
	"Wallet Password":                {T: "Hasło portfela"},
	"w_password_helper":              {T: "To hasło, które zostało skonfigurowane w oprogramowaniu Twojego portfela."},
	"w_password_tooltip":             {T: "Zostaw puste pole, jeśli portfel nie wymaga użycia hasła."},
	"App Password":                   {T: "Hasło aplikacji"},
	"Add":                            {T: "Dodaj"},
	"Unlock":                         {T: "Odblokuj"},
	"Wallet":                         {T: "Portfel"},
	"app_password_reminder":          {T: "Hasło aplikacji jest wymagane zawsze wtedy, gdy wykonuje się wrażliwe operacje z użyciem portfela."},
	"DEX Address":                    {T: "Adres DEX"},
	"TLS Certificate":                {T: "Certyfikat TLS"},
	"remove":                         {T: "usuń"},
	"add a file":                     {T: "dodaj plik"},
	"Submit":                         {T: "Wyślij"},
	"Confirm Registration":           {T: "Potwierdź rejestrację"},
	"app_pw_reg":                     {T: "Podaj hasło aplikacji, aby potwierdzić rejestrację na DEX."},
	"reg_confirm_submit":             {T: `Po wysłaniu tego formularza środki z Twojego portfela zostaną wykorzystane do zapłacenia opłaty rejestracyjnej.`},
	"provided_markets":               {T: "Ten DEX obsługuje następujące rynki:"},
	"accepted_fee_assets":            {T: "Ten DEX przyjmuje następujące opłaty:"},
	"base_header":                    {T: "Waluta bazowa"},
	"quote_header":                   {T: "Waluta kwotowana"},
	"lot_size_headsup":               {T: "Wszystkie wymiany przeprowadzane są w zakresie wielokrotności rozmiaru lotu."},
	"Password":                       {T: "Hasło"},
	"Register":                       {T: "Zarejestruj"},
	"Authorize Export":               {T: "Zatwierdź eksport"},
	"export_app_pw_msg":              {T: "Podaj hasło aplikacji, aby potwierdzić eksport konta dla"},
	"Disable Account":                {T: "Zablokuj konto"},
	"disable_app_pw_msg":             {T: "Podaj hasło aplikacji, aby zablokowac konto"},
	"disable_dex_server":             {T: "Ten serwer DEX może zostać ponownie włączony w dowolnym momencie w przyszłości, dodając go ponownie."},
	"Authorize Import":               {T: "Zatwierdź import"},
	"app_pw_import_msg":              {T: "Podaj hasło aplikacji, aby potwierdzić import konta"},
	"Account File":                   {T: "Plik konta"},
	"Change Application Password":    {T: "Zmień hasło aplikacji"},
	"Current Password":               {T: "Obecne hasło"},
	"New Password":                   {T: "Nowe hasło"},
	"Confirm New Password":           {T: "Potwierdź nowe hasło"},
	"cancel_no_pw":                   {T: "Wyślij zlecenie anulowania pozostałej sumy"},
	"cancel_remain":                  {T: "Pozostałą suma może ulec zmianie, zanim zlecenie anulowania zostanie wykonane."},
	"Log In":                         {T: "Zaloguj się"},
	"epoch":                          {T: "epoka"},
	"price":                          {T: "cena"},
	"volume":                         {T: "wolumen"},
	"buys":                           {T: "kupno"},
	"sells":                          {T: "sprzedaż"},
	"Buy Orders":                     {T: "Zlecenia kupna"},
	"Quantity":                       {T: "Ilość"},
	"Rate":                           {T: "Kurs"},
	"Limit Order":                    {T: "Zlecenie oczekujące (limit)"},
	"Market Order":                   {T: "Zlecenie rynkowe (market)"},
	"reg_status_msg":                 {T: `Żeby rozpocząć handel na <span id="regStatusDex" class="text-break"></span>, opłata rejestracyjna potrzebuje <span id="confReq"></span> potwierdzeń.`},
	"Buy":                            {T: "Kupno"},
	"Sell":                           {T: "Sprzedaż"},
	"lot_size":                       {T: "Rozmiar lotu"},
	"Rate Step":                      {T: "Różnica ceny zlecenia"},
	"Max":                            {T: "Max"},
	"lot":                            {T: "lot"},
	"min trade is about":             {T: "minimalna wielkość zamówienia wynosi około"},
	"immediate_explanation":          {T: "Jeśli zamówienie nie zostanie w pełni spasowane w następnym cyklu, pozostałe środki nie będą wystawiane ani pasowane ponownie. Zlecenie typu 'taker-only'."},
	"Immediate or cancel":            {T: "Immediate or cancel"},
	"Balances":                       {T: "Salda"},
	"outdated_tooltip":               {T: "Saldo może być nieaktualne. Połącz się z portfelem, aby je odświeżyć."},
	"available":                      {T: "dostępne"},
	"connect_refresh_tooltip":        {T: "Kliknij, aby połączyć i odświeżyć"},
	"add_a_wallet":                   {T: `Dodaj portfel <span data-tmpl="addWalletSymbol"></span> `},
	"locked":                         {T: "zablokowane"},
	"immature":                       {T: "niedojrzałe"},
	"Sell Orders":                    {T: "Zlecenia sprzedaży"},
	"Your Orders":                    {T: "Twoje zlecenia"},
	"Type":                           {T: "Rodzaj"},
	"Side":                           {T: "Strona"},
	"Age":                            {T: "Wiek"},
	"Filled":                         {T: "Zrealizowane"},
	"Settled":                        {T: "Rozliczone"},
	"Status":                         {T: "Status"},
	"view order history":             {T: "wyświetl historię zleceń"},
	"cancel_order":                   {T: "anuluj zlecenie"},
	"order details":                  {T: "szczegóły zlecenia"},
	"verify_order":                   {T: `Zweryfikuj zlecenie <span id="vSideHeader"></span>`},
	"You are submitting an order to": {T: "Wysyłasz zlecenie"},
	"at a rate of":                   {T: "po kursie"},
	"for a total of":                 {T: "na sumę"},
	"verify_market":                  {T: "Zlecenie rynkowe, które spasowywane jest z najlepszymi dostępnymi zleceniami w księdze zleceń. W oparciu o obecny kurs między stronami może otrzymać około"},
	"auth_order_app_pw":              {T: "Potwierdź to zlecenie swoim hasłem aplikacji."},
	"lots":                           {T: "loty(ów)"},
	"order_disclaimer": {T: `<span class="red">UWAGA</span>: Wymiany potrzebują czasu, aby zostać rozliczone. NIE WYŁĄCZAJ swojego klienta DEX, ani oprogramowania, czy portfela blockchaina dla
		<span data-unit="quote"></span> lub <span data-unit="base"></span>, dopóki
		rozliczenie nie zostanie dokonane. Rozliczenie może trwać od kilku minut, aż do kilku godzin.`},
	"Order":                      {T: "Zlecenie"},
	"see all orders":             {T: "wyświetl wszystkie zlecenia"},
	"Exchange":                   {T: "Giełda"},
	"Market":                     {T: "Rynek"},
	"Offering":                   {T: "Oferta"},
	"Asking":                     {T: "W zamian za"},
	"Fees":                       {T: "Opłaty"},
	"order_fees_tooltip":         {T: "Opłaty transakcyjne on-chain, zazwyczaj zbierane przez górników. Decred DEX nie pobiera żadnych opłat handlowych."},
	"Matches":                    {T: "Spasowane zlecenia"},
	"Match ID":                   {T: "ID zlecenia"},
	"Time":                       {T: "Czas"},
	"ago":                        {T: "temu"},
	"Cancellation":               {T: "Anulowano"},
	"Order Portion":              {T: "Część zlecenia"},
	"you":                        {T: "Ty"},
	"them":                       {T: "oni"},
	"Redemption":                 {T: "Wykupienie środków"},
	"Refund":                     {T: "Zwrot środków"},
	"Funding Coins":              {T: "Środki fundujące zlecenie"},
	"Exchanges":                  {T: "Giełdy"},
	"apply":                      {T: "zastosuj"},
	"Assets":                     {T: "Aktywa"},
	"Trade":                      {T: "Wymiana"},
	"Set App Password":           {T: "Ustaw hasło aplikacji"},
	"reg_set_app_pw_msg":         {T: "Ustaw swoje hasło aplikacji. To hasło będzie chronić klucze Twojego konta DEX oraz połączone z nim portfele."},
	"Password Again":             {T: "Wprowadź hasło ponownie"},
	"Add a DEX":                  {T: "Dodaj DEX"},
	"reg_ssl_needed":             {T: "Wygląda na to, że nie posiadamy certyfikatu SSL dla tego DEXa. Dodaj certyfikat serwera, aby przejść dalej."},
	"Dark Mode":                  {T: "Tryb ciemny"},
	"Show pop-up notifications":  {T: "Pokazuj powiadomienia w okienkach"},
	"Account ID":                 {T: "ID konta"},
	"Export Account":             {T: "Eksportuj konto"},
	"simultaneous_servers_msg":   {T: "Klient Decred DEX wspiera jednoczesne korzystanie z wielu serwerów DEX."},
	"Change App Password":        {T: "Zmień hasło aplikacji"},
	"Build ID":                   {T: "ID builda"},
	"Connect":                    {T: "Połącz"},
	"Withdraw":                   {T: "Wypłać"},
	"Deposit":                    {T: "Zdeponuj"},
	"Lock":                       {T: "Zablokuj"},
	"New Deposit Address":        {T: "Nowy adres do depozytów"},
	"Address":                    {T: "Adres"},
	"Amount":                     {T: "Ilość"},
	"Reconfigure":                {T: "Skonfiguruj ponownie"},
	"pw_change_instructions":     {T: "Zmiana poniższego hasła nie powoduje zmiany hasła do Twojego oprogramowania portfela. Skorzystaj z tego formularza, aby zaktualizować klienta DEX po tym, jak zmienisz hasło do swojego oprogramowania portfela."},
	"New Wallet Password":        {T: "Nowe hasło portfela"},
	"pw_change_warn":             {T: "Uwaga: Zmiana portfela podczas gdy trwają wymiany może spowodować utratę środków."},
	"Show more options":          {T: "Wyświetl więcej opcji"},
	"seed_implore_msg":           {T: "Zapisz ziarno aplikacji dokładnie na kartce papieru i zachowaj jego kopię. Jeśli stracisz dostęp do tego urządzenia lub niezbędnych plików aplikacji, ziarno to umożliwi Ci przywrócenie kont DEX oraz wbudowanych portfeli. Niektóre starsze konta nie mogę być przywrócone ta metodą, i niezależenie od tego, czy konto jest nowe, czy stare, zachowanie kopii swoich kluczy w dodatku do ziarna jest zawsze dobrą praktyką."},
	"View Application Seed":      {T: "Wyświetl ziarno aplikacji"},
	"Remember my password":       {T: "Zapamiętaj hasło"},
	"pw_for_seed":                {T: "Enter your app password to show your seed. Make sure nobody else can see your screen."},
	"Asset":                      {T: "Aktywo"},
	"Balance":                    {T: "Saldo"},
	"Actions":                    {T: "Czynności"},
	"Restoration Seed":           {T: "Ziarno do przywrócenia"},
	"Restore from seed":          {T: "Przywróć z ziarna"},
	"Import Account":             {T: "Importuj konto"},
	"no_wallet":                  {T: "brak portfela"},
	"create_a_x_wallet":          {T: "Utwórz portfel <span data-asset-name=1></span>"},
	"dont_share":                 {T: "Nie udostępniaj nikomu. Nie zgub go."},
	"Show Me":                    {T: "Pokaż"},
	"Wallet Settings":            {T: "Ustawienia portfela"},
	"add_a_x_wallet":             {T: `Dodaj portfel <img data-tmpl="assetLogo" class="asset-logo mx-1"> <span data-tmpl="assetName"></span>`},
	"ready":                      {T: "gotowy"},
	"off":                        {T: "wyłączony"},
	"Export Trades":              {T: "Eksportuj zlecenia wymiany"},
	"change the wallet type":     {T: "zmień typ portfela"},
	"confirmations":              {T: "potwierdzenia"},
	"pick a different asset":     {T: "wybierz inne aktywo"},
	"Create":                     {T: "Utwórz"},
	"1 Sync the Blockchain":      {T: "1: Zsynchronizuj blockchain"},
	"Progress":                   {T: "Postęp"},
	"remaining":                  {T: "pozostało"},
	"Your Deposit Address":       {T: "Twój adres do wpłaty"},
	"add a different server":     {T: "dodaj inny serwer"},
	"Add a custom server":        {T: "Dodaj niestandardowy serwer"},
	"plus tx fees":               {T: "+ opłaty transakcyjne"},
	"Export Seed":                {T: "Eksportuj ziarno"},
	"Total":                      {T: "W sumie"},
	"Trading":                    {T: "Wymiana"},
	"Receiving Approximately":    {T: "Otrzymując około"},
	"Fee Projection":             {T: "Szacunkowa opłata"},
	"details":                    {T: "szczegóły"},
	"to":                         {T: "do"},
	"Options":                    {T: "Opcje"},
	"fee_projection_tooltip":     {T: "Jeśli warunki sieciowe nie ulegną zmianie zanim Twoje zlecenie zostanie spasowane, całkowite poniesione opłaty powinny zmieścić się w tym przedziale."},
	"unlock_for_details":         {T: "Odblokuj portfele, aby wyciągnąć dane zleceń oraz dodatkowe opcje."},
	"estimate_unavailable":       {T: "Szacunkowe dane zleceń i opcje są niedostępne"},
	"Fee Details":                {T: "Szczegóły opłat"},
	"estimate_market_conditions": {T: "Dane szacunkowe dla najlepszego i najgorszego scenariusza oparte są na obecnych warunkach sieciowych i mogą ulec zmianie do czasu wykonania zlecenia."},
	"Best Case Fees":             {T: "Opłaty (najlepszy scenariusz)"},
	"best_case_conditions":       {T: "Najlepszy scenariusz dla opłat występuje wtedy, gdy zlecenie jest zrealizowane w całości jednym spasowaniem."},
	"Swap":                       {T: "Zamiana"},
	"Redeem":                     {T: "Wykupienie"},
	"Worst Case Fees":            {T: "Opłaty (najgorszy scenariusz)"},
	"worst_case_conditions":      {T: "Najgorszy scenariusz dla opłat może wystąpić wtedy, gdy zlecenie jest spasowane po jednym locie na przestrzeni kilku epok."},
	"Maximum Possible Swap Fees": {T: "Maksymalne możliwe opłaty wymiany"},
	"max_fee_conditions":         {T: "To absolutne maksimum tego, co możesz zapłacić przy swojej wymianie. Opłaty są zazwyczaj wyceniane na ułamek tej kwoty. Maksimum nie ulega zmianie po złożeniu zlecenia."},
}
