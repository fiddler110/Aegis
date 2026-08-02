class CanaryController < ApplicationController
  def index
    @users = User.where("name = '#{params[:name]}'")
    render inline: params[:template]
  end

  def run
    eval(params[:code])
  end
end
