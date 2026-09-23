#!/bin/bash

###
# Скрипт установки и первичной настройки golang-monitoring client
###

#source "./newt_colors.sh"

#проверка на запуск от sudo/root
check_sudo(){
if [[ $EUID -ne 0 ]]; then
	echo -e "\\e[31mНеобходисо запустить скрипт от sudo или root\\e[0m"
	exit 1
fi
}

#проверка на наличие whiptail(псевдографика)
check_install_whiptail(){
    if ! command -v whiptail &> /dev/null; then
		echo "whiptail не установлен"
		read -p "Хотите установить whiptail? (y/n): " f3
			if [ "$f3" = "y" ] || [ "$f3" = "Y" ]; then
				if sudo apt update && sudo apt install whiptail -y; then
					echo "whiptail установлен"
				else
					echo -e "\\e[31mwhiptail не установлен, попробуйте установить вручную\\e[0m"
					exit 1
				fi
			else
				echo -e "\\e[33mУстановка отменена\\e[0m"
				echo -e "\\e[31mбез whiptail скрипт не запустится неисправно\\e[0m"
				exit 1
			fi
    fi
}

show_main_menu() {
check_sudo


apt install golang -y


check_install_whiptail


if whiptail --title "Выбор установки" --yesno "начать установку monitoring client?" 10 40; then
	whiptail --title "Уведомление" --msgbox "Запуск установки" 10 40
    else
	echo "Пользователь отказался от установки, завершение..."
	exit 1

fi

get_ethn() {
    IP1=$(whiptail --title "IP сервера" --inputbox "Введите имя ip сервера" 10 60 "192.168.*" 3>&1 1>&2 2>&3)
    if [ $? -ne 0 ] || [ -z "$IP1" ]; then
        whiptail --title "Ошибка" --msgbox "ip не может быть пустым!" 10 60
        return 1
    fi
    return 0
}


(
echo "XXX"
echo 30
echo "Настройка сети"
echo "XXX"

get_ethn

echo "XXX"
echo 60
echo "Копирование фалов"
echo "XXX"

chmod +x ./opt/astra-agent/agent
sed -i "s|\"server_url\": \"http://[0-9.]*:8080|\"server_url\": \"http://${IP1}:8080|" ./opt/astra-agent/config.json
cp -r ./opt /

rm -fr /opt/astra-agent/agent.log
touch /opt/astra-agent/agent.log
chmod 777 /opt/astra-agent/agent.log

echo "XXX"
echo 90
echo "Настройка агента"
echo "XXX"

cp astra-agent.service /etc/systemd/system/

echo "XXX"
echo 95
echo "Запуск агента"
echo "XXX"

systemctl start astra-agent
sleep 1


echo "XXX"
echo 100
echo "Установка завершена"
echo "XXX"

) | whiptail --title "ustanovka" --gauge "pocess instaling" 10 70 0

}
show_main_menu

systemctl daemon-reload
sleep 1
systemctl status astra-agent